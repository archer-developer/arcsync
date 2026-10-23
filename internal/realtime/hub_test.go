package realtime

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"arcsync/internal/room"
	"arcsync/internal/syncclock"

	"github.com/gorilla/websocket"
)

func newTestServer(t *testing.T) (*room.Room, *httptest.Server) {
	t.Helper()
	r := room.New("ROOM01", "host-token")
	hub := NewHub(r, syncclock.Real(), nil)
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := up.Upgrade(w, req, nil)
		if err != nil {
			return
		}
		c := NewClient(hub, conn, r.ID)
		hub.Register(c)
		go c.WritePump()
		go c.ReadPump()
	}))
	t.Cleanup(srv.Close)
	return r, srv
}

func dial(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func readType(t *testing.T, conn *websocket.Conn, want string) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var m map[string]any
		if err := conn.ReadJSON(&m); err != nil {
			t.Fatalf("waiting for %q: %v", want, err)
		}
		if m["type"] == want {
			return m
		}
	}
}

func TestJoin_returnsIdentityAndToken(t *testing.T) {
	_, srv := newTestServer(t)
	c := dial(t, srv)
	_ = c.WriteJSON(map[string]any{"type": "join_room", "nickname": "Same", "host_token": "host-token"})
	st := readType(t, c, "room_state")
	if st["you"] == "" || st["reconnect_token"] == "" {
		t.Fatalf("missing identity: %v", st)
	}
	if st["host_id"] != st["you"] {
		t.Errorf("host token holder should be host: %v", st)
	}

	// A second player with the same nickname gets a distinct identity.
	c2 := dial(t, srv)
	_ = c2.WriteJSON(map[string]any{"type": "join_room", "nickname": "Same"})
	st2 := readType(t, c2, "room_state")
	if st2["you"] == st["you"] || st2["host_id"] == st2["you"] {
		t.Errorf("duplicate nickname must not share identity or host: %v", st2)
	}
}

func TestReconnect_staleConnectionDoesNotDisconnect(t *testing.T) {
	r, srv := newTestServer(t)

	old := dial(t, srv)
	_ = old.WriteJSON(map[string]any{"type": "join_room", "nickname": "A"})
	st := readType(t, old, "room_state")
	pid, token := st["you"].(string), st["reconnect_token"].(string)

	// The public participant ID must not be usable to take over a session.
	evil := dial(t, srv)
	_ = evil.WriteJSON(map[string]any{"type": "join_room", "nickname": "X", "reconnect_token": pid})
	if st := readType(t, evil, "room_state"); st["you"] == pid {
		t.Fatal("participant ID was accepted as reconnect token")
	}

	fresh := dial(t, srv)
	_ = fresh.WriteJSON(map[string]any{"type": "join_room", "nickname": "A", "reconnect_token": token})
	if st := readType(t, fresh, "room_state"); st["you"] != pid {
		t.Fatalf("reconnect should restore identity, got %v", st["you"])
	}

	_ = old.Close() // stale socket finally dies
	time.Sleep(200 * time.Millisecond)
	p, _ := r.GetParticipant(pid)
	if !p.Connected {
		t.Fatal("participant must stay connected after the stale socket closes")
	}
}

func TestJoin_twiceRejected(t *testing.T) {
	_, srv := newTestServer(t)
	c := dial(t, srv)
	_ = c.WriteJSON(map[string]any{"type": "join_room", "nickname": "A"})
	readType(t, c, "room_state")
	_ = c.WriteJSON(map[string]any{"type": "join_room", "nickname": "A"})
	e := readType(t, c, "error")
	b, _ := json.Marshal(e)
	if !strings.Contains(string(b), "already_joined") {
		t.Fatalf("unexpected error: %s", b)
	}
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) events() []map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(l.b.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

func (l *lockedBuf) find(msg string) map[string]any {
	for _, e := range l.events() {
		if e["msg"] == msg {
			return e
		}
	}
	return nil
}

func TestEventLog_joinLatencyCountdownLeave(t *testing.T) {
	r := room.New("ROOM01", "host-token")
	buf := &lockedBuf{}
	hub := NewHub(r, syncclock.Real(), slog.New(slog.NewJSONHandler(buf, nil)))
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := up.Upgrade(w, req, nil)
		if err != nil {
			return
		}
		c := NewClient(hub, conn, r.ID)
		hub.Register(c)
		go c.WritePump()
		go c.ReadPump()
	}))
	defer srv.Close()

	c := dial(t, srv)
	_ = c.WriteJSON(map[string]any{"type": "join_room", "nickname": "Alice", "host_token": "host-token"})
	readType(t, c, "room_state")

	j := buf.find("participant_joined")
	if j == nil || j["nickname"] != "Alice" || j["is_host"] != true || j["reconnect"] != false {
		t.Fatalf("bad join event: %v", j)
	}

	_ = c.WriteJSON(map[string]any{"type": "time_sync", "client_send_ts": 1, "client_now_unix_ms": time.Now().UnixMilli(), "rtt_ms": 42})
	readType(t, c, "time_sync_result")
	_ = c.WriteJSON(map[string]any{"type": "set_ready", "ready": true})
	readType(t, c, "participant_updated")
	_ = c.WriteJSON(map[string]any{"type": "start_countdown", "start_at": time.Now().Add(10 * time.Second).UnixMilli()})
	readType(t, c, "countdown_started")

	if l := buf.find("latency_measured"); l == nil || l["rtt_ms"] != float64(42) || l["nickname"] != "Alice" {
		t.Fatalf("bad latency event: %v", l)
	}
	cs := buf.find("countdown_started")
	if cs == nil || cs["connected"] != float64(1) {
		t.Fatalf("bad countdown event: %v", cs)
	}
	ps, _ := cs["participants"].([]any)
	if len(ps) != 1 || ps[0].(map[string]any)["rtt_ms"] != float64(42) {
		t.Fatalf("countdown participants missing latency: %v", cs["participants"])
	}

	_ = c.Close()
	deadline := time.Now().Add(2 * time.Second)
	for buf.find("participant_left") == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if l := buf.find("participant_left"); l == nil || l["nickname"] != "Alice" || l["rtt_ms"] != float64(42) {
		t.Fatalf("bad leave event: %v", l)
	}
}

package realtime

import (
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"arcsync/internal/room"
	"arcsync/internal/syncclock"
)

const maxPlausibleRTTMS = 10000

// Hub manages all WebSocket clients in a single room.
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]bool   // all currently connected clients
	byPID   map[string]*Client // participantID → client

	r     *room.Room
	clock syncclock.Clock
}

// NewHub creates a Hub for the given room.
func NewHub(r *room.Room, clock syncclock.Clock) *Hub {
	return &Hub{
		clients: make(map[*Client]bool),
		byPID:   make(map[string]*Client),
		r:       r,
		clock:   clock,
	}
}

// Register adds a new, unjoined client to the hub.
func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
}

// unregister removes a client and marks its participant disconnected.
func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	owner := c.participantID != "" && h.byPID[c.participantID] == c
	if owner {
		delete(h.byPID, c.participantID)
	}
	h.mu.Unlock()
	close(c.done)

	// A newer connection may already own this participant (fast reconnect);
	// in that case the participant must stay connected.
	if !owner {
		return
	}

	cancelled := h.r.RemoveParticipant(c.participantID)
	p, ok := h.r.GetParticipant(c.participantID)
	if !ok {
		return
	}
	pv := participantView(p)

	if cancelled {
		// Host disconnected during countdown — broadcast cancellation then left
		h.broadcastAll(CountdownCancelledMsg{
			Type:    MsgCountdownCancelled,
			RoomID:  h.r.ID,
			RoundID: h.r.CurrentRoundID(),
		})
	}

	h.broadcastAll(ParticipantLeftMsg{
		Type:          MsgParticipantLeft,
		RoomID:        h.r.ID,
		ParticipantID: c.participantID,
		Connected:     pv.Connected,
	})
}

// handleMessage routes an incoming raw message to the correct handler.
func (h *Hub) handleMessage(c *Client, data []byte) {
	var base ClientBase
	if err := json.Unmarshal(data, &base); err != nil {
		c.sendJSON(ErrorMsg{Type: MsgError, Code: "invalid_json", Message: "invalid JSON"})
		return
	}

	switch base.Type {
	case MsgJoinRoom:
		var m JoinRoomMsg
		_ = json.Unmarshal(data, &m)
		h.handleJoin(c, m)
	case MsgSetReady:
		if c.participantID == "" {
			c.sendJSON(ErrorMsg{Type: MsgError, Code: "not_joined", Message: "send join_room first"})
			return
		}
		var m SetReadyMsg
		_ = json.Unmarshal(data, &m)
		h.handleSetReady(c, m)
	case MsgStartCountdown:
		if c.participantID == "" {
			c.sendJSON(ErrorMsg{Type: MsgError, Code: "not_joined", Message: "send join_room first"})
			return
		}
		var m StartCountdownMsg
		_ = json.Unmarshal(data, &m)
		h.handleStartCountdown(c, m)
	case MsgCancelCountdown:
		if c.participantID == "" {
			c.sendJSON(ErrorMsg{Type: MsgError, Code: "not_joined", Message: "send join_room first"})
			return
		}
		var m CancelCountdownMsg
		_ = json.Unmarshal(data, &m)
		h.handleCancelCountdown(c, m)
	case MsgTimeSync:
		var m TimeSyncMsg
		_ = json.Unmarshal(data, &m)
		h.handleTimeSync(c, m)
	case MsgPing:
		c.sendJSON(struct {
			Type string `json:"type"`
		}{Type: MsgPong})
	default:
		c.sendJSON(ErrorMsg{Type: MsgError, Code: "unknown_type",
			Message: "unknown message type: " + base.Type})
	}
}

// handleJoin processes a join_room message.
func (h *Hub) handleJoin(c *Client, m JoinRoomMsg) {
	if c.participantID != "" {
		c.sendJSON(ErrorMsg{Type: MsgError, Code: "already_joined", Message: "already joined"})
		return
	}

	nick := strings.TrimSpace(m.Nickname)
	nick, err := room.ValidateNickname(nick)
	if err != nil {
		c.sendJSON(ErrorMsg{Type: MsgError, Code: "bad_nickname", Message: err.Error()})
		return
	}

	// Attempt reconnect with the secret token
	if pid, ok := h.r.ReconnectParticipant(m.ReconnectToken); ok {
		c.participantID = pid
		h.mu.Lock()
		old := h.byPID[pid]
		h.byPID[pid] = c
		h.mu.Unlock()
		if old != nil && old != c {
			_ = old.conn.Close() // drop the stale connection
		}
		p, _ := h.r.GetParticipant(pid)
		c.sendJSON(h.roomStateMsg(p))
		h.broadcastExcept(c, ParticipantUpdatedMsg{
			Type:        MsgParticipantUpdated,
			RoomID:      h.r.ID,
			Participant: participantView(p),
		})
		return
	}
	// Reconnect failed or not requested; continue as a new participant

	pid, err := room.NewParticipantID()
	if err != nil {
		c.sendJSON(ErrorMsg{Type: MsgError, Code: "internal", Message: "could not generate participant ID"})
		return
	}
	token, err := room.NewReconnectToken()
	if err != nil {
		c.sendJSON(ErrorMsg{Type: MsgError, Code: "internal", Message: "could not generate token"})
		return
	}

	p := &room.Participant{ID: pid, Token: token, Nickname: nick}
	if err := h.r.AddParticipant(p, m.HostToken); err != nil {
		c.sendJSON(ErrorMsg{Type: MsgError, Code: "room_full", Message: err.Error()})
		return
	}
	c.participantID = pid

	h.mu.Lock()
	h.byPID[pid] = c
	h.mu.Unlock()

	joined, _ := h.r.GetParticipant(pid)
	c.sendJSON(h.roomStateMsg(joined))
	h.broadcastExcept(c, ParticipantJoinedMsg{
		Type:        MsgParticipantJoined,
		RoomID:      h.r.ID,
		Participant: participantView(joined),
	})
}

// handleSetReady processes a set_ready message.
func (h *Hub) handleSetReady(c *Client, m SetReadyMsg) {
	if err := h.r.SetReady(c.participantID, m.Ready); err != nil {
		c.sendJSON(ErrorMsg{Type: MsgError, Code: "bad_state", Message: err.Error()})
		return
	}
	p, _ := h.r.GetParticipant(c.participantID)
	h.broadcastAll(ParticipantUpdatedMsg{
		Type:        MsgParticipantUpdated,
		RoomID:      h.r.ID,
		Participant: participantView(p),
	})
}

// handleStartCountdown processes a start_countdown message.
func (h *Hub) handleStartCountdown(c *Client, m StartCountdownMsg) {
	now := h.clock.Now()
	startAt, goUntil, err := h.r.StartCountdown(c.participantID, m.StartAt, now)
	if err != nil {
		c.sendJSON(ErrorMsg{Type: MsgError, Code: "start_failed", Message: err.Error()})
		return
	}
	snap := h.r.GetSnapshot()
	roundID := snap.RoundID

	h.broadcastAll(CountdownStartedMsg{
		Type:       MsgCountdownStarted,
		RoomID:     h.r.ID,
		RoundID:    roundID,
		StartAt:    startAt,
		GoUntil:    goUntil,
		DurationMS: snap.DurationMS,
	})

	// Schedule GO transition
	delay := time.UnixMilli(startAt).Sub(h.clock.Now())
	goTimer := h.clock.AfterFunc(delay, func() {
		h.transitionToGo(roundID)
	})
	h.r.SetCancelFunc(func() { goTimer.Stop() })
}

// transitionToGo is called by the clock timer.
func (h *Hub) transitionToGo(roundID int64) {
	if err := h.r.TransitionToGo(roundID); err != nil {
		return // stale round
	}

	snap := h.r.GetSnapshot()
	h.broadcastAll(GoMsg{
		Type:    MsgGo,
		RoomID:  h.r.ID,
		RoundID: roundID,
		GoUntil: snap.GoUntil,
	})

	// Schedule LOBBY transition
	delay := time.UnixMilli(snap.GoUntil).Sub(h.clock.Now())
	h.clock.AfterFunc(delay, func() {
		h.transitionToLobby(roundID)
	})
}

// transitionToLobby is called by the clock timer.
func (h *Hub) transitionToLobby(roundID int64) {
	if err := h.r.TransitionToLobby(roundID); err != nil {
		return // stale round
	}

	h.broadcastAll(CountdownResetMsg{
		Type:       MsgCountdownReset,
		RoomID:     h.r.ID,
		RoundID:    roundID,
		DurationMS: h.r.GetSnapshot().DurationMS,
	})
}

// handleCancelCountdown processes a cancel_countdown message.
func (h *Hub) handleCancelCountdown(c *Client, m CancelCountdownMsg) {
	if err := h.r.CancelCountdown(c.participantID, m.RoundID); err != nil {
		c.sendJSON(ErrorMsg{Type: MsgError, Code: "cancel_failed", Message: err.Error()})
		return
	}
	h.broadcastAll(CountdownCancelledMsg{
		Type:    MsgCountdownCancelled,
		RoomID:  h.r.ID,
		RoundID: m.RoundID,
	})
}

// handleTimeSync responds to a clock-sync probe and updates RTT + clock offset.
func (h *Hub) handleTimeSync(c *Client, m TimeSyncMsg) {
	recvTS := h.clock.Now().UnixMilli()
	c.sendJSON(TimeSyncResultMsg{
		Type:         MsgTimeSyncResult,
		ClientSendTS: m.ClientSendTS,
		ServerRecvTS: recvTS,
		ServerSendTS: h.clock.Now().UnixMilli(),
	})
	if c.participantID == "" {
		return
	}
	changed := false
	if m.RttMs > 0 && m.RttMs <= maxPlausibleRTTMS {
		changed = h.r.UpdateRTT(c.participantID, m.RttMs) || changed
	}
	if m.ClientNowUnixMs > 0 {
		// client clock − server clock (positive = client is ahead); the probe
		// reached the server about half an RTT after the client stamped it.
		oneWay := m.RttMs / 2
		if m.RttMs <= 0 || m.RttMs > maxPlausibleRTTMS {
			oneWay = 0
		}
		changed = h.r.UpdateClockOffset(c.participantID, m.ClientNowUnixMs-(recvTS-oneWay)) || changed
	}
	// Only broadcast when the visible values moved noticeably; probes run every
	// couple of seconds per client and would otherwise flood the room.
	if changed {
		if p, ok := h.r.GetParticipant(c.participantID); ok {
			h.broadcastAll(ParticipantUpdatedMsg{
				Type:        MsgParticipantUpdated,
				RoomID:      h.r.ID,
				Participant: participantView(p),
			})
		}
	}
}

// --- broadcast helpers ---

func (h *Hub) broadcastAll(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("broadcast marshal error: %v", err)
		return
	}
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	for _, c := range clients {
		select {
		case c.send <- data:
		default:
		}
	}
}

func (h *Hub) broadcastExcept(exclude *Client, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		if c != exclude {
			clients = append(clients, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range clients {
		select {
		case c.send <- data:
		default:
		}
	}
}

// roomStateMsg builds a full RoomStateMsg addressed to the participant `you`.
func (h *Hub) roomStateMsg(you room.Participant) RoomStateMsg {
	snap := h.r.GetSnapshot()
	pvs := make([]ParticipantView, 0, len(snap.Participants))
	for _, p := range snap.Participants {
		pvs = append(pvs, participantView(*p))
	}
	return RoomStateMsg{
		Type:           MsgRoomState,
		RoomID:         snap.RoomID,
		State:          string(snap.State),
		HostID:         snap.HostID,
		You:            you.ID,
		ReconnectToken: you.Token,
		StartAt:        snap.StartAt,
		GoUntil:        snap.GoUntil,
		RoundID:        snap.RoundID,
		DurationMS:     snap.DurationMS,
		Participants:   pvs,
	}
}

func participantView(p room.Participant) ParticipantView {
	return ParticipantView{
		ID:            p.ID,
		Nickname:      p.Nickname,
		Ready:         p.Ready,
		RTTMS:         p.RTTMS,
		ClockOffsetMS: p.ClockOffsetMS,
		Connected:     p.Connected,
	}
}

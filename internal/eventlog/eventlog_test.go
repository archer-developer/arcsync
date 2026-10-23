package eventlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNew_writesJSONLinesToDailyFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs") // must be created on demand
	lg, closer, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	lg.Info("room_created", "room_id", "ABC123")
	_ = closer.Close()

	name := "events-" + time.Now().UTC().Format("2006-01-02") + ".log"
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	line := string(data)
	if !strings.Contains(line, `"msg":"room_created"`) || !strings.Contains(line, `"room_id":"ABC123"`) {
		t.Fatalf("unexpected log line: %s", line)
	}
}

func TestDailyWriter_rotatesOnNewDay(t *testing.T) {
	dir := t.TempDir()
	cur := time.Date(2026, 1, 1, 23, 59, 0, 0, time.UTC)
	w := &dailyWriter{dir: dir, now: func() time.Time { return cur }}
	if _, err := w.Write([]byte("a\n")); err != nil {
		t.Fatal(err)
	}
	cur = cur.Add(2 * time.Minute)
	if _, err := w.Write([]byte("b\n")); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	for _, n := range []string{"events-2026-01-01.log", "events-2026-01-02.log"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Errorf("missing %s: %v", n, err)
		}
	}
}

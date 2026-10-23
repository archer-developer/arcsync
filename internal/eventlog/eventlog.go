// Package eventlog writes structured usage events (JSON lines) to daily files.
package eventlog

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Discard returns a logger that drops everything.
func Discard() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// New returns a logger that appends JSON lines to <dir>/events-YYYY-MM-DD.log
// (UTC date), creating dir if needed. Close releases the current file.
func New(dir string) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	w := &dailyWriter{dir: dir, now: time.Now}
	if err := w.rotate(); err != nil {
		return nil, nil, err
	}
	return slog.New(slog.NewJSONHandler(w, nil)), w, nil
}

type dailyWriter struct {
	dir string
	now func() time.Time

	mu   sync.Mutex
	day  string
	file *os.File
}

func (w *dailyWriter) rotate() error {
	day := w.now().UTC().Format("2006-01-02")
	if w.file != nil && day == w.day {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(w.dir, fmt.Sprintf("events-%s.log", day)),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if w.file != nil {
		_ = w.file.Close()
	}
	w.file, w.day = f, day
	return nil
}

func (w *dailyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.rotate(); err != nil {
		return 0, err
	}
	return w.file.Write(p)
}

func (w *dailyWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

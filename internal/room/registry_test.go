package room

import (
	"regexp"
	"testing"
	"time"
)

var validIDRe = regexp.MustCompile(`^[A-Z0-9]{6}$`)

func TestGenerateID_format(t *testing.T) {
	for range 50 {
		id, err := generateID()
		if err != nil {
			t.Fatal(err)
		}
		if !validIDRe.MatchString(id) {
			t.Fatalf("invalid room ID format: %q", id)
		}
	}
}

func TestRegistry_create(t *testing.T) {
	rg := NewRegistry()
	r, err := rg.Create()
	if err != nil {
		t.Fatal(err)
	}
	if !validIDRe.MatchString(r.ID) {
		t.Fatalf("invalid room ID: %q", r.ID)
	}
	if r.HostToken == "" {
		t.Fatal("host token must not be empty")
	}
}

func TestRegistry_uniqueness(t *testing.T) {
	rg := NewRegistry()
	seen := make(map[string]bool)
	for range 100 {
		r, err := rg.Create()
		if err != nil {
			t.Fatal(err)
		}
		if seen[r.ID] {
			t.Fatalf("duplicate room ID: %q", r.ID)
		}
		seen[r.ID] = true
	}
}

func TestRegistry_get(t *testing.T) {
	rg := NewRegistry()
	r, _ := rg.Create()

	got, err := rg.Get(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != r {
		t.Fatal("Get returned different room pointer")
	}
}

func TestRegistry_notFound(t *testing.T) {
	rg := NewRegistry()
	_, err := rg.Get("XXXXXX")
	if err != ErrRoomNotFound {
		t.Fatalf("expected ErrRoomNotFound, got %v", err)
	}
}

func TestRegistry_delete(t *testing.T) {
	rg := NewRegistry()
	r, _ := rg.Create()
	rg.Delete(r.ID)
	_, err := rg.Get(r.ID)
	if err != ErrRoomNotFound {
		t.Fatal("room should be deleted")
	}
}

func TestNewParticipantID_format(t *testing.T) {
	hexRe := regexp.MustCompile(`^[0-9a-f]{16}$`)
	for range 20 {
		id, err := NewParticipantID()
		if err != nil {
			t.Fatal(err)
		}
		if !hexRe.MatchString(id) {
			t.Fatalf("invalid participant ID: %q", id)
		}
	}
}

func TestRegistry_sweep(t *testing.T) {
	rg := NewRegistry()
	r, err := rg.Create()
	if err != nil {
		t.Fatal(err)
	}
	if got := rg.Sweep(time.Minute, time.Minute); len(got) != 0 {
		t.Fatalf("fresh room swept: %v", got)
	}
	r.mu.Lock()
	r.lastActivity = time.Now().Add(-time.Hour)
	r.mu.Unlock()
	if got := rg.Sweep(time.Minute, time.Minute); len(got) != 1 || got[0] != r.ID {
		t.Fatalf("idle room not swept: %v", got)
	}
	if _, err := rg.Get(r.ID); err != ErrRoomNotFound {
		t.Fatalf("room should be gone, got %v", err)
	}
}

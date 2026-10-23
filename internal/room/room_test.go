package room

import (
	"strings"
	"testing"
	"time"
)

func makeRoom() *Room {
	return New("TESTR1", "secret-token")
}

func addParticipant(t *testing.T, r *Room, id, nick string, host bool) {
	t.Helper()
	p := &Participant{ID: id, Nickname: nick}
	token := ""
	if host {
		token = r.HostToken
	}
	_ = r.AddParticipant(p, token)
}

// --- ValidateNickname ---

func TestValidateNickname_empty(t *testing.T) {
	_, err := ValidateNickname("")
	if err != ErrNicknameEmpty {
		t.Fatalf("expected ErrNicknameEmpty, got %v", err)
	}
}

func TestValidateNickname_tooLong(t *testing.T) {
	_, err := ValidateNickname(strings.Repeat("a", 21))
	if err != ErrNicknameTooLong {
		t.Fatalf("expected ErrNicknameTooLong, got %v", err)
	}
}

func TestValidateNickname_maxLen(t *testing.T) {
	s := strings.Repeat("a", 20)
	got, err := ValidateNickname(s)
	if err != nil {
		t.Fatal(err)
	}
	if got != s {
		t.Fatalf("expected %q, got %q", s, got)
	}
}

func TestValidateNickname_unicode(t *testing.T) {
	// 20 emoji = 20 runes, must be accepted
	s := strings.Repeat("🎮", 20)
	_, err := ValidateNickname(s)
	if err != nil {
		t.Fatal(err)
	}
	// 21 emoji = 21 runes, must be rejected
	s21 := strings.Repeat("🎮", 21)
	_, err = ValidateNickname(s21)
	if err != ErrNicknameTooLong {
		t.Fatalf("expected ErrNicknameTooLong, got %v", err)
	}
}

// --- Participant management ---

func TestAddParticipant_setsConnected(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "p1", "Alice", false)
	p, ok := r.GetParticipant("p1")
	if !ok || !p.Connected {
		t.Fatal("participant should be connected after add")
	}
}

func TestAddParticipant_hostToken(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "h1", "Host", true)
	if r.HostID != "h1" {
		t.Fatalf("HostID should be h1, got %q", r.HostID)
	}
}

func TestAddParticipant_wrongToken(t *testing.T) {
	r := makeRoom()
	p := &Participant{ID: "h1", Nickname: "Host"}
	_ = r.AddParticipant(p, "wrong-token")
	if r.HostID != "" {
		t.Fatal("wrong token should not grant host")
	}
}

func TestRemoveParticipant_setsDisconnected(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "p1", "Alice", false)
	r.RemoveParticipant("p1")
	p, _ := r.GetParticipant("p1")
	if p.Connected {
		t.Fatal("participant should be disconnected after remove")
	}
}

func TestRemoveParticipant_resetsReady(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "h1", "Host", true)
	addParticipant(t, r, "p1", "Alice", false)
	_ = r.SetReady("p1", true)
	r.RemoveParticipant("p1")
	p, _ := r.GetParticipant("p1")
	if p.Ready {
		t.Fatal("ready should be reset on disconnect")
	}
}

// --- Readiness ---

func TestSetReady_requiresLobby(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "h1", "Host", true)
	addParticipant(t, r, "p1", "Alice", false)
	_ = r.SetReady("h1", true)
	_ = r.SetReady("p1", true)
	now := time.Now()
	_, _, _ = r.StartCountdown("h1", now.Add(MinSchedulingLead+DefaultDuration).UnixMilli(), now)

	err := r.SetReady("p1", false)
	if err != ErrNotInLobby {
		t.Fatalf("expected ErrNotInLobby, got %v", err)
	}
}

// --- StartCountdown ---

func TestStartCountdown_requiresHost(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "h1", "Host", true)
	addParticipant(t, r, "p1", "Alice", false)
	_ = r.SetReady("h1", true)
	_ = r.SetReady("p1", true)

	now := time.Now()
	_, _, err := r.StartCountdown("p1", now.Add(MinSchedulingLead+DefaultDuration).UnixMilli(), now)
	if err != ErrNotHost {
		t.Fatalf("expected ErrNotHost, got %v", err)
	}
}

func TestStartCountdown_requiresAllReady(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "h1", "Host", true)
	addParticipant(t, r, "p1", "Alice", false)
	_ = r.SetReady("h1", true)
	// p1 not ready

	now := time.Now()
	_, _, err := r.StartCountdown("h1", now.Add(MinSchedulingLead+DefaultDuration).UnixMilli(), now)
	if err != ErrParticipantsNotReady {
		t.Fatalf("expected ErrParticipantsNotReady, got %v", err)
	}
}

func TestStartCountdown_requiresLobbyState(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "h1", "Host", true)
	_ = r.SetReady("h1", true)
	now := time.Now()
	sa := now.Add(MinSchedulingLead + DefaultDuration).UnixMilli()
	_, _, _ = r.StartCountdown("h1", sa, now)

	_, _, err := r.StartCountdown("h1", sa, now)
	if err != ErrNotInLobby {
		t.Fatalf("expected ErrNotInLobby, got %v", err)
	}
}

func TestStartCountdown_success(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "h1", "Host", true)
	addParticipant(t, r, "p1", "Alice", false)
	_ = r.SetReady("h1", true)
	_ = r.SetReady("p1", true)

	now := time.Now()
	startAtMS := now.Add(MinSchedulingLead + DefaultDuration).UnixMilli()
	sa, gu, err := r.StartCountdown("h1", startAtMS, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.CurrentState() != StateCountdown {
		t.Fatal("state should be COUNTDOWN")
	}
	if r.CurrentRoundID() != 1 {
		t.Fatalf("roundID should be 1, got %d", r.CurrentRoundID())
	}
	if sa != startAtMS {
		t.Fatalf("startAt mismatch: got %d, want %d", sa, startAtMS)
	}
	expectedGo := startAtMS + GoStateDuration.Milliseconds()
	if gu != expectedGo {
		t.Fatalf("goUntil mismatch: got %d, want %d", gu, expectedGo)
	}
}

func TestStartCountdown_startTimeTooSoon(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "h1", "Host", true)
	_ = r.SetReady("h1", true)

	now := time.Now()
	_, _, err := r.StartCountdown("h1", now.UnixMilli()+500, now) // 500ms in future
	if err != ErrStartTimeTooSoon {
		t.Fatalf("expected ErrStartTimeTooSoon, got %v", err)
	}
}

func TestStartCountdown_startTimeTooFar(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "h1", "Host", true)
	_ = r.SetReady("h1", true)

	now := time.Now()
	_, _, err := r.StartCountdown("h1", now.Add(48*time.Hour).UnixMilli(), now)
	if err != ErrStartTimeTooFar {
		t.Fatalf("expected ErrStartTimeTooFar, got %v", err)
	}
}

func TestStartCountdown_disconnectedParticipantIgnored(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "h1", "Host", true)
	addParticipant(t, r, "p1", "Alice", false)
	_ = r.SetReady("h1", true)
	r.RemoveParticipant("p1") // disconnected, not ready

	now := time.Now()
	_, _, err := r.StartCountdown("h1", now.Add(MinSchedulingLead+DefaultDuration).UnixMilli(), now)
	if err != nil {
		t.Fatalf("disconnected participant should not block start: %v", err)
	}
}

// --- CancelCountdown ---

func TestCancelCountdown_requiresHost(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "h1", "Host", true)
	addParticipant(t, r, "p1", "Alice", false)
	_ = r.SetReady("h1", true)
	_ = r.SetReady("p1", true)
	_, _, _ = r.StartCountdown("h1", time.Now().Add(MinSchedulingLead+DefaultDuration).UnixMilli(), time.Now())

	err := r.CancelCountdown("p1", r.RoundID)
	if err != ErrNotHost {
		t.Fatalf("expected ErrNotHost, got %v", err)
	}
}

func TestCancelCountdown_staleRound(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "h1", "Host", true)
	_ = r.SetReady("h1", true)
	_, _, _ = r.StartCountdown("h1", time.Now().Add(MinSchedulingLead+DefaultDuration).UnixMilli(), time.Now())

	err := r.CancelCountdown("h1", 999)
	if err != ErrRoundMismatch {
		t.Fatalf("expected ErrRoundMismatch, got %v", err)
	}
}

func TestCancelCountdown_success(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "h1", "Host", true)
	_ = r.SetReady("h1", true)
	_, _, _ = r.StartCountdown("h1", time.Now().Add(MinSchedulingLead+DefaultDuration).UnixMilli(), time.Now())

	err := r.CancelCountdown("h1", r.RoundID)
	if err != nil {
		t.Fatal(err)
	}
	if r.CurrentState() != StateLobby {
		t.Fatal("state should return to LOBBY after cancel")
	}
}

// --- TransitionToGo / TransitionToLobby ---

func TestTransitionToGo_staleRound(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "h1", "Host", true)
	_ = r.SetReady("h1", true)
	_, _, _ = r.StartCountdown("h1", time.Now().Add(MinSchedulingLead+DefaultDuration).UnixMilli(), time.Now())

	err := r.TransitionToGo(999)
	if err != ErrRoundMismatch {
		t.Fatalf("expected ErrRoundMismatch, got %v", err)
	}
}

func TestTransitionToGo_success(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "h1", "Host", true)
	_ = r.SetReady("h1", true)
	roundID := int64(0)
	_, _, _ = r.StartCountdown("h1", time.Now().Add(MinSchedulingLead+DefaultDuration).UnixMilli(), time.Now())
	roundID = r.RoundID

	if err := r.TransitionToGo(roundID); err != nil {
		t.Fatal(err)
	}
	if r.CurrentState() != StateGo {
		t.Fatal("state should be GO")
	}
}

func TestTransitionToLobby_preservesReadiness(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "h1", "Host", true)
	addParticipant(t, r, "p1", "Alice", false)
	_ = r.SetReady("h1", true)
	_ = r.SetReady("p1", true)
	_, _, _ = r.StartCountdown("h1", time.Now().Add(MinSchedulingLead+DefaultDuration).UnixMilli(), time.Now())
	roundID := r.RoundID
	_ = r.TransitionToGo(roundID)
	_ = r.TransitionToLobby(roundID)

	if r.CurrentState() != StateLobby {
		t.Fatal("state should be LOBBY")
	}
	// Readiness preserved
	h, _ := r.GetParticipant("h1")
	p, _ := r.GetParticipant("p1")
	if !h.Ready || !p.Ready {
		t.Fatal("readiness should be preserved after round completes")
	}
}

func TestRoundIDMonotonicallyIncreases(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "h1", "Host", true)
	_ = r.SetReady("h1", true)

	for i := int64(1); i <= 3; i++ {
		_, _, _ = r.StartCountdown("h1", time.Now().Add(MinSchedulingLead+DefaultDuration).UnixMilli(), time.Now())
		if r.RoundID != i {
			t.Fatalf("expected roundID %d, got %d", i, r.RoundID)
		}
		_ = r.TransitionToGo(r.RoundID)
		_ = r.TransitionToLobby(r.RoundID)
	}
}

// --- Host disconnect ---

func TestHostDisconnect_duringCountdown_cancels(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "h1", "Host", true)
	_ = r.SetReady("h1", true)
	_, _, _ = r.StartCountdown("h1", time.Now().Add(MinSchedulingLead+DefaultDuration).UnixMilli(), time.Now())

	cancelled := r.RemoveParticipant("h1")
	if !cancelled {
		t.Fatal("expected countdown to be cancelled on host disconnect")
	}
	if r.CurrentState() != StateLobby {
		t.Fatal("state should be LOBBY after host disconnect")
	}
}

// --- Snapshot ---

func TestGetSnapshot_consistency(t *testing.T) {
	r := makeRoom()
	addParticipant(t, r, "h1", "Host", true)
	addParticipant(t, r, "p1", "Alice", false)

	snap := r.GetSnapshot()
	if snap.RoomID != "TESTR1" {
		t.Fatalf("wrong room ID in snapshot: %s", snap.RoomID)
	}
	if len(snap.Participants) != 2 {
		t.Fatalf("expected 2 participants, got %d", len(snap.Participants))
	}
}

func TestReconnectParticipant_byToken(t *testing.T) {
	r := New("ROOM01", "tok")
	p := &Participant{ID: "p1", Token: "secret", Nickname: "A"}
	_ = r.AddParticipant(p, "")
	r.RemoveParticipant("p1")

	if _, ok := r.ReconnectParticipant("p1"); ok {
		t.Fatal("participant ID must not work as a reconnect token")
	}
	if _, ok := r.ReconnectParticipant(""); ok {
		t.Fatal("empty token must not match")
	}
	id, ok := r.ReconnectParticipant("secret")
	if !ok || id != "p1" {
		t.Fatalf("reconnect by token failed: %q %v", id, ok)
	}
	got, _ := r.GetParticipant("p1")
	if !got.Connected {
		t.Error("participant should be connected after reconnect")
	}
}

func TestAddParticipant_roomFull(t *testing.T) {
	r := New("ROOM01", "tok")
	for i := 0; i < MaxParticipants; i++ {
		if err := r.AddParticipant(&Participant{ID: string(rune('a' + i)), Nickname: "n"}, ""); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	if err := r.AddParticipant(&Participant{ID: "extra", Nickname: "n"}, ""); err != ErrRoomFull {
		t.Fatalf("want ErrRoomFull, got %v", err)
	}
}

func TestUpdateTiming_reportsOnlyNoticeableChanges(t *testing.T) {
	r := New("ROOM01", "tok")
	_ = r.AddParticipant(&Participant{ID: "p1", Nickname: "A"}, "")

	if !r.UpdateClockOffset("p1", 100) {
		t.Error("first offset measurement should count as a change")
	}
	if r.UpdateClockOffset("p1", 103) {
		t.Error("3 ms offset jitter should not be reported")
	}
	if !r.UpdateClockOffset("p1", 150) {
		t.Error("50 ms offset change should be reported")
	}
	if !r.UpdateRTT("p1", 40) {
		t.Error("RTT 0 -> 40 should be reported")
	}
	if r.UpdateRTT("p1", 42) {
		t.Error("2 ms RTT jitter should not be reported")
	}
}

func TestExpired(t *testing.T) {
	r := New("ROOM01", "tok")
	now := time.Now()
	if r.Expired(time.Minute, now) {
		t.Error("fresh room must not be expired")
	}
	if !r.Expired(time.Minute, now.Add(2*time.Minute)) {
		t.Error("idle empty room should expire")
	}
	_ = r.AddParticipant(&Participant{ID: "p1", Nickname: "A"}, "")
	if r.Expired(time.Minute, now.Add(time.Hour)) {
		t.Error("room with a connected participant must not expire")
	}
}

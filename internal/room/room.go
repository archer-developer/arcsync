package room

import (
	"errors"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	DefaultDuration     = 10 * time.Second
	MinSchedulingLead   = 3 * time.Second // minimum lead time for host-supplied absolute startAt
	MaxSchedulingWindow = 24 * time.Hour  // reject startAt values too far in the future
	GoStateDuration     = 5 * time.Second
	MaxNicknameLen      = 20
	GracePeriod         = 30 * time.Second
)

type State string

const (
	StateLobby     State = "LOBBY"
	StateCountdown State = "COUNTDOWN"
	StateGo        State = "GO"
)

var (
	ErrNotHost              = errors.New("only the host can perform this action")
	ErrNotInLobby           = errors.New("room is not in lobby state")
	ErrNotInCountdown       = errors.New("room is not in countdown state")
	ErrNotInGo              = errors.New("room is not in go state")
	ErrParticipantsNotReady = errors.New("not all participants are ready")
	ErrRoundMismatch        = errors.New("stale round ID")
	ErrNicknameEmpty        = errors.New("nickname cannot be empty")
	ErrNicknameTooLong      = errors.New("nickname too long (max 20 characters)")
	ErrStartTimeTooSoon     = errors.New("start time must be at least 3 seconds in the future")
	ErrStartTimeTooFar      = errors.New("start time must be within 24 hours")
	ErrHostDisconnected     = errors.New("host is disconnected")
	ErrParticipantNotFound  = errors.New("participant not found")
)

// Participant is one connected user.
type Participant struct {
	ID             string `json:"id"`
	Nickname       string `json:"nickname"`
	Ready          bool   `json:"ready"`
	RTTMS          int64  `json:"rtt_ms"`
	ClockOffsetMS  *int64 `json:"clock_offset_ms,omitempty"` // estimated UTC clock offset vs server; nil = not yet measured
	Connected      bool   `json:"connected"`
	disconnectedAt time.Time
}

// Room holds all shared state for a sync-start session.
type Room struct {
	ID        string
	HostID    string // participant ID of the host
	HostToken string // secret issued at room creation

	State    State
	Duration time.Duration

	StartAt int64 // Unix ms when countdown reaches zero
	GoUntil int64 // Unix ms when GO state ends
	RoundID int64 // monotonically increasing; 0 = no round yet

	participants map[string]*Participant
	mu           sync.RWMutex

	cancelCountdown func() // cancel the active countdown goroutines
}

// New creates a Room in LOBBY state.
func New(id, hostToken string) *Room {
	return &Room{
		ID:           id,
		HostToken:    hostToken,
		State:        StateLobby,
		Duration:     DefaultDuration,
		participants: make(map[string]*Participant),
	}
}

// ValidateNickname trims and validates. Returns cleaned nickname or error.
func ValidateNickname(raw string) (string, error) {
	// Trim is done in caller; here we only validate post-trim.
	if raw == "" {
		return "", ErrNicknameEmpty
	}
	if utf8.RuneCountInString(raw) > MaxNicknameLen {
		return "", ErrNicknameTooLong
	}
	return raw, nil
}

// AddParticipant adds or re-activates a participant.
// Returns ErrNotHost if hostToken is given and doesn't match.
func (r *Room) AddParticipant(p *Participant, hostToken string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p.Connected = true
	r.participants[p.ID] = p
	if hostToken != "" && hostToken == r.HostToken && r.HostID == "" {
		r.HostID = p.ID
	}
}

// RemoveParticipant marks a participant as disconnected.
// If the host disconnects during a countdown, the countdown is cancelled.
func (r *Room) RemoveParticipant(participantID string) (cancelledRound bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.participants[participantID]
	if !ok {
		return false
	}
	p.Connected = false
	p.Ready = false
	p.disconnectedAt = time.Now()

	// Host disconnect during countdown → cancel
	if participantID == r.HostID && r.State == StateCountdown {
		r.doCancelCountdown()
		return true
	}
	return false
}

// ReconnectParticipant marks an existing participant as connected again.
// Returns false if not found.
func (r *Room) ReconnectParticipant(participantID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.participants[participantID]
	if !ok {
		return false
	}
	p.Connected = true
	return true
}

// SetReady toggles readiness for one participant.
func (r *Room) SetReady(participantID string, ready bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.participants[participantID]
	if !ok {
		return ErrParticipantNotFound
	}
	if r.State != StateLobby {
		return ErrNotInLobby
	}
	p.Ready = ready
	return nil
}

// UpdateRTT records a new RTT measurement for a participant.
func (r *Room) UpdateRTT(participantID string, rttMS int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.participants[participantID]; ok {
		p.RTTMS = rttMS
	}
}

// UpdateClockOffset stores the estimated UTC clock offset (client − server, in ms) for a participant.
func (r *Room) UpdateClockOffset(participantID string, offsetMS int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.participants[participantID]; ok {
		v := offsetMS
		p.ClockOffsetMS = &v
	}
}

// StartCountdown attempts to move the room from LOBBY to COUNTDOWN.
// startAtMS is the host-supplied absolute start time (Unix ms, UTC).
// Returns StartAt and GoUntil (Unix ms) on success.
func (r *Room) StartCountdown(hostID string, startAtMS int64, now time.Time) (startAt, goUntil int64, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.HostID != hostID {
		return 0, 0, ErrNotHost
	}
	if r.State != StateLobby {
		return 0, 0, ErrNotInLobby
	}
	for _, p := range r.participants {
		if p.Connected && !p.Ready {
			return 0, 0, ErrParticipantsNotReady
		}
	}

	minStart := now.UnixMilli() + MinSchedulingLead.Milliseconds()
	if startAtMS < minStart {
		return 0, 0, ErrStartTimeTooSoon
	}
	maxStart := now.Add(MaxSchedulingWindow).UnixMilli()
	if startAtMS > maxStart {
		return 0, 0, ErrStartTimeTooFar
	}

	r.RoundID++
	r.Duration = time.Duration(startAtMS-now.UnixMilli()) * time.Millisecond
	r.StartAt = startAtMS
	r.GoUntil = startAtMS + GoStateDuration.Milliseconds()
	r.State = StateCountdown

	return r.StartAt, r.GoUntil, nil
}

// CancelCountdown moves from COUNTDOWN to LOBBY.
func (r *Room) CancelCountdown(hostID string, roundID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.HostID != hostID {
		return ErrNotHost
	}
	if r.State != StateCountdown {
		return ErrNotInCountdown
	}
	if r.RoundID != roundID {
		return ErrRoundMismatch
	}
	r.doCancelCountdown()
	return nil
}

// doCancelCountdown is the inner cancel, called with lock held.
func (r *Room) doCancelCountdown() {
	if r.cancelCountdown != nil {
		r.cancelCountdown()
		r.cancelCountdown = nil
	}
	r.State = StateLobby
}

// SetCancelFunc stores the cancel function for the active countdown goroutines.
func (r *Room) SetCancelFunc(cancel func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancelCountdown = cancel
}

// TransitionToGo moves COUNTDOWN → GO. Returns ErrRoundMismatch if stale.
func (r *Room) TransitionToGo(roundID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State != StateCountdown || r.RoundID != roundID {
		return ErrRoundMismatch
	}
	r.State = StateGo
	return nil
}

// TransitionToLobby moves GO → LOBBY. Returns ErrRoundMismatch if stale.
func (r *Room) TransitionToLobby(roundID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State != StateGo || r.RoundID != roundID {
		return ErrRoundMismatch
	}
	r.State = StateLobby
	r.StartAt = 0
	r.GoUntil = 0
	// Readiness is preserved per ADR §6
	return nil
}

// Snapshot holds a read-only view of the room for broadcasting.
type Snapshot struct {
	RoomID       string         `json:"room_id"`
	State        State          `json:"state"`
	HostID       string         `json:"host_id"`
	StartAt      int64          `json:"start_at"`
	GoUntil      int64          `json:"go_until"`
	RoundID      int64          `json:"round_id"`
	DurationMS   int64          `json:"duration_ms"`
	Participants []*Participant `json:"participants"`
}

// GetSnapshot returns a read-consistent copy of room state.
func (r *Room) GetSnapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ps := make([]*Participant, 0, len(r.participants))
	for _, p := range r.participants {
		cp := *p
		ps = append(ps, &cp)
	}
	return Snapshot{
		RoomID:       r.ID,
		State:        r.State,
		HostID:       r.HostID,
		StartAt:      r.StartAt,
		GoUntil:      r.GoUntil,
		RoundID:      r.RoundID,
		DurationMS:   r.Duration.Milliseconds(),
		Participants: ps,
	}
}

// GetParticipant returns a copy of a participant or false if not found.
func (r *Room) GetParticipant(id string) (Participant, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.participants[id]
	if !ok {
		return Participant{}, false
	}
	return *p, true
}

// RoundID returns the current round ID (safe concurrent read).
func (r *Room) CurrentRoundID() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.RoundID
}

// CurrentState returns room state safely.
func (r *Room) CurrentState() State {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.State
}

// IsHostConnected reports whether the host participant is currently connected.
func (r *Room) IsHostConnected() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.participants[r.HostID]
	return ok && p.Connected
}

// PurgeStale removes disconnected participants whose grace period has expired.
func (r *Room) PurgeStale(grace time.Duration) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := time.Now().Add(-grace)
	var removed []string
	for id, p := range r.participants {
		if !p.Connected && p.disconnectedAt.Before(cutoff) {
			delete(r.participants, id)
			removed = append(removed, id)
		}
	}
	return removed
}

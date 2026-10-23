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
	if c.participantID != "" {
		delete(h.byPID, c.participantID)
	}
	close(c.send)
	h.mu.Unlock()

	if c.participantID == "" {
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
	// Normalize nickname
	nick := strings.TrimSpace(m.Nickname)
	nick, err := room.ValidateNickname(nick)
	if err != nil {
		c.sendJSON(ErrorMsg{Type: MsgError, Code: "bad_nickname", Message: err.Error()})
		return
	}

	// Attempt reconnect
	if m.ReconnectToken != "" {
		if ok := h.r.ReconnectParticipant(m.ReconnectToken); ok {
			c.participantID = m.ReconnectToken
			h.mu.Lock()
			h.byPID[c.participantID] = c
			h.mu.Unlock()
			c.sendJSON(h.roomStateMsg())
			p, _ := h.r.GetParticipant(c.participantID)
			h.broadcastExcept(c, ParticipantJoinedMsg{
				Type:        MsgParticipantJoined,
				RoomID:      h.r.ID,
				Participant: participantView(p),
			})
			return
		}
		// Reconnect failed; continue as new participant
	}

	pid, err := room.NewParticipantID()
	if err != nil {
		c.sendJSON(ErrorMsg{Type: MsgError, Code: "internal", Message: "could not generate participant ID"})
		return
	}

	p := &room.Participant{
		ID:       pid,
		Nickname: nick,
	}
	h.r.AddParticipant(p, m.HostToken)
	c.participantID = pid

	h.mu.Lock()
	h.byPID[pid] = c
	h.mu.Unlock()

	// Send full room state to the joining client
	c.sendJSON(h.roomStateMsg())

	// Broadcast participant_joined to everyone else
	joined, _ := h.r.GetParticipant(pid)
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
	roundID := h.r.CurrentRoundID()
	dur := h.r.Duration

	h.broadcastAll(CountdownStartedMsg{
		Type:       MsgCountdownStarted,
		RoomID:     h.r.ID,
		RoundID:    roundID,
		StartAt:    startAt,
		GoUntil:    goUntil,
		DurationMS: dur.Milliseconds(),
	})

	// Schedule GO transition
	delay := time.Until(time.UnixMilli(startAt))
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
	delay := time.Until(time.UnixMilli(snap.GoUntil))
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
		DurationMS: h.r.Duration.Milliseconds(),
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
	sendTS := h.clock.Now().UnixMilli()
	c.sendJSON(TimeSyncResultMsg{
		Type:         MsgTimeSyncResult,
		ClientSendTS: m.ClientSendTS,
		ServerRecvTS: recvTS,
		ServerSendTS: sendTS,
	})
	if c.participantID == "" {
		return
	}
	updated := false
	if m.RttMs > 0 {
		h.r.UpdateRTT(c.participantID, m.RttMs)
		updated = true
	}
	if m.ClientNowUnixMs > 0 {
		// Estimate: client clock − server clock (positive = client is ahead)
		h.r.UpdateClockOffset(c.participantID, m.ClientNowUnixMs-recvTS)
		updated = true
	}
	if updated {
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

// roomStateMsg builds a full RoomStateMsg from the current snapshot.
func (h *Hub) roomStateMsg() RoomStateMsg {
	snap := h.r.GetSnapshot()
	pvs := make([]ParticipantView, 0, len(snap.Participants))
	for _, p := range snap.Participants {
		pvs = append(pvs, participantView(*p))
	}
	return RoomStateMsg{
		Type:         MsgRoomState,
		RoomID:       snap.RoomID,
		State:        string(snap.State),
		HostID:       snap.HostID,
		StartAt:      snap.StartAt,
		GoUntil:      snap.GoUntil,
		RoundID:      snap.RoundID,
		DurationMS:   snap.DurationMS,
		Participants: pvs,
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

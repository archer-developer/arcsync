package realtime

// Client → Server message types.
const (
	MsgJoinRoom        = "join_room"
	MsgSetReady        = "set_ready"
	MsgStartCountdown  = "start_countdown"
	MsgCancelCountdown = "cancel_countdown"
	MsgTimeSync        = "time_sync"
	MsgPing            = "ping"
)

// Server → Client message types.
const (
	MsgRoomState          = "room_state"
	MsgParticipantJoined  = "participant_joined"
	MsgParticipantUpdated = "participant_updated"
	MsgParticipantLeft    = "participant_left"
	MsgCountdownStarted   = "countdown_started"
	MsgCountdownCancelled = "countdown_cancelled"
	MsgGo                 = "go"
	MsgCountdownReset     = "countdown_reset"
	MsgTimeSyncResult     = "time_sync_result"
	MsgError              = "error"
	MsgPong               = "pong"
)

// ClientBase holds just the type field for routing.
type ClientBase struct {
	Type string `json:"type"`
}

// JoinRoomMsg is sent by a client to join a room.
type JoinRoomMsg struct {
	Type           string `json:"type"`
	RoomID         string `json:"room_id"`
	Nickname       string `json:"nickname"`
	HostToken      string `json:"host_token,omitempty"`
	ReconnectToken string `json:"reconnect_token,omitempty"`
}

// SetReadyMsg toggles readiness.
type SetReadyMsg struct {
	Type  string `json:"type"`
	Ready bool   `json:"ready"`
}

// StartCountdownMsg asks the server to schedule the countdown to an absolute start time.
type StartCountdownMsg struct {
	Type    string `json:"type"`
	StartAt int64  `json:"start_at"` // host-supplied absolute start time (Unix ms, UTC)
}

// CancelCountdownMsg asks the server to cancel the countdown.
type CancelCountdownMsg struct {
	Type    string `json:"type"`
	RoundID int64  `json:"round_id"`
}

// TimeSyncMsg is the clock-sync probe.
type TimeSyncMsg struct {
	Type            string  `json:"type"`
	ClientSendTS    float64 `json:"client_send_ts"`     // performance.now() ms
	ClientNowUnixMs int64   `json:"client_now_unix_ms"` // Date.now() ms for clock-offset estimation
	RttMs           int64   `json:"rtt_ms"`             // latest RTT estimate from client
}

// --- Server → Client ---

// ParticipantView is what the client sees for each participant.
type ParticipantView struct {
	ID            string `json:"id"`
	Nickname      string `json:"nickname"`
	Ready         bool   `json:"ready"`
	RTTMS         int64  `json:"rtt_ms"`
	ClockOffsetMS *int64 `json:"clock_offset_ms,omitempty"` // nil = not yet measured
	Connected     bool   `json:"connected"`
}

// RoomStateMsg is the authoritative full snapshot.
type RoomStateMsg struct {
	Type         string            `json:"type"`
	RoomID       string            `json:"room_id"`
	State        string            `json:"state"`
	HostID       string            `json:"host_id"`
	StartAt      int64             `json:"start_at"`
	GoUntil      int64             `json:"go_until"`
	RoundID      int64             `json:"round_id"`
	DurationMS   int64             `json:"duration_ms"`
	Participants []ParticipantView `json:"participants"`
}

// ParticipantJoinedMsg notifies the room of a new participant.
type ParticipantJoinedMsg struct {
	Type        string          `json:"type"`
	RoomID      string          `json:"room_id"`
	Participant ParticipantView `json:"participant"`
}

// ParticipantUpdatedMsg notifies the room of a participant state change.
type ParticipantUpdatedMsg struct {
	Type        string          `json:"type"`
	RoomID      string          `json:"room_id"`
	Participant ParticipantView `json:"participant"`
}

// ParticipantLeftMsg notifies the room that a participant disconnected.
type ParticipantLeftMsg struct {
	Type          string `json:"type"`
	RoomID        string `json:"room_id"`
	ParticipantID string `json:"participant_id"`
	Connected     bool   `json:"connected"`
}

// CountdownStartedMsg is broadcast when a countdown begins.
type CountdownStartedMsg struct {
	Type       string `json:"type"`
	RoomID     string `json:"room_id"`
	RoundID    int64  `json:"round_id"`
	StartAt    int64  `json:"start_at"`
	GoUntil    int64  `json:"go_until"`
	DurationMS int64  `json:"duration_ms"`
}

// CountdownCancelledMsg is broadcast when the host cancels.
type CountdownCancelledMsg struct {
	Type    string `json:"type"`
	RoomID  string `json:"room_id"`
	RoundID int64  `json:"round_id"`
}

// GoMsg is broadcast when the countdown reaches zero.
type GoMsg struct {
	Type    string `json:"type"`
	RoomID  string `json:"room_id"`
	RoundID int64  `json:"round_id"`
	GoUntil int64  `json:"go_until"`
}

// CountdownResetMsg is broadcast when GO state ends and lobby resumes.
type CountdownResetMsg struct {
	Type       string `json:"type"`
	RoomID     string `json:"room_id"`
	RoundID    int64  `json:"round_id"`
	DurationMS int64  `json:"duration_ms"`
}

// TimeSyncResultMsg responds to a time_sync probe.
type TimeSyncResultMsg struct {
	Type         string  `json:"type"`
	ClientSendTS float64 `json:"client_send_ts"`
	ServerRecvTS int64   `json:"server_recv_ts"` // Unix ms
	ServerSendTS int64   `json:"server_send_ts"` // Unix ms
}

// ErrorMsg reports a server-side error to the client.
type ErrorMsg struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

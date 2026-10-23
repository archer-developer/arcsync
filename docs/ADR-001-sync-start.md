# ADR-001: Sync Start --- Synchronized Multiplayer Start Countdown

-   **Status:** Proposed
-   **Date:** 2026-10-23
-   **Audience:** Claude Code implementation agent
-   **Scope:** MVP

## 1. Context

Sync Start is a lightweight web application that helps a group of
players press the start button in a cooperative game as simultaneously
as practical. One user acts as the host/streamer and controls the
countdown. Other users join the same room, choose a nickname, see the
participants' latency and readiness, and manually press the game button
when the countdown reaches zero.

The application must be mobile-first, minimal, and visually distinctive:
dark high-tech gaming aesthetic, restrained neon accents, and a clear
full-screen green `GO!` state.

The application does **not** automate game input and does not rely on
YouTube or Discord APIs.

## 2. Decision

Build a single Go web service that serves embedded static assets and
provides a WebSocket endpoint. The frontend uses HTML, CSS, and vanilla
JavaScript only.

### Technology choices

-   Backend: Go.
-   Realtime transport: WebSocket.
-   Frontend: semantic HTML, modern CSS, vanilla JavaScript.
-   Static assets: embedded in the Go binary using `embed`.
-   Initial state storage: in-memory room registry; no database or Redis
    in the MVP.
-   Deployment: one process/container behind HTTPS-capable reverse proxy
    or platform ingress.
-   No frontend framework and no client-side build step required for the
    initial implementation.

Claude Code must inspect the existing repository before implementing.
Preserve established conventions where they exist; do not overwrite
unrelated code. If the repository is empty, use the structure below.

Suggested structure:

``` text
cmd/server/main.go
internal/room/
internal/realtime/
internal/syncclock/
web/index.html
web/css/styles.css
web/js/app.js
web/js/socket.js
web/js/timer.js
go.mod
Dockerfile
README.md
```

## 3. Roles and permissions

### Host / streamer

The user who creates a room becomes its host.

The host can: - Create a room. - Copy/share the room URL and
six-character room code. - See all connected participants, their
nicknames, readiness, and measured RTT. - Start the countdown. - Cancel
a running countdown. - Start another countdown after the previous round
returns to the lobby state.

Only the host can start or cancel a countdown. Host identity is
associated with the connection/session that created the room in the MVP.
Do not allow another participant to claim host privileges merely by
sending a client message. If the host disconnects, do not silently
promote another player; mark the room as host-disconnected and disable
countdown controls until the host reconnects or the room is handled by a
clearly defined recovery policy.

### Player

A player can: - Join a room using its link or room code. - Set a
nickname. - See all participants, their readiness, and their measured
RTT. - Toggle their own status between Ready and Not Ready. - See the
shared countdown. - See the green full-screen `GO!` signal when the
countdown reaches zero.

Players cannot start or cancel the countdown.

## 4. Room identity and joining

-   Room IDs are exactly six characters.
-   Allowed characters: `A-Z` and `0-9` only (uppercase ASCII letters
    and digits).
-   Generate IDs using a cryptographically secure random source.
-   Enforce uniqueness by checking the in-memory registry and retrying
    on collision.
-   A room URL should be directly shareable, for example `/room/7F3A2B`.
-   The join flow should support both opening a shared URL and entering
    a room code manually.
-   Normalize manually entered room codes to uppercase, then validate
    the exact format.
-   If a room does not exist, show a concise room-not-found state and a
    path to create/join another room.
-   Do not invent a fixed participant limit unless required by an
    existing repository constraint. If a practical limit is introduced,
    make it configurable and return a clear error when reached.

## 5. Nicknames and participant identity

-   A nickname is required before joining the room participant list.
-   Trim surrounding whitespace and reject empty names.
-   Apply a reasonable maximum length (recommended: 20 Unicode
    characters).
-   Render nicknames as text, never as raw HTML.
-   The server assigns an opaque participant ID; do not use the nickname
    as a unique identity.
-   Duplicate nicknames may be allowed, because participant identity is
    separate. If duplicate names are allowed, disambiguate visually with
    a small secondary marker rather than rejecting them.
-   On a reconnect, attempt to restore the participant identity for the
    same browser session if practical. Do not let a user impersonate
    another participant by submitting their participant ID without a
    valid session token.

## 6. Readiness behavior

-   New participants start as **Not Ready**.
-   Each player can toggle between **Ready** and **Not Ready**.
-   Readiness is visible to everyone in the room.
-   The host's readiness is displayed like any other participant.
-   Starting a countdown requires all currently connected participants
    to be Ready, including the host.
-   If anyone is Not Ready or disconnected, the server rejects the start
    request and the UI explains why.
-   While a countdown is running, readiness controls are disabled. This
    avoids readiness changes creating ambiguous countdown behavior.
-   When the countdown is cancelled or completes, readiness values are
    preserved, allowing the host to start another round without
    requiring everyone to toggle Ready again.
-   A disconnected participant is marked disconnected/not-ready for
    start eligibility. Remove stale disconnected participants after a
    configurable grace period or room cleanup period; do not immediately
    erase them if a brief reconnect is likely.

## 7. Countdown lifecycle

Default countdown duration is **10 seconds**.

The MVP may use a fixed 10-second duration. Keep the duration
configurable in code, and if a duration control is implemented, default
it to 10 seconds and validate a safe range (recommended: 3--60 seconds).
Do not add extra controls unless they improve the stated workflow.

Room states:

-   `LOBBY`: no countdown is running; countdown display is reset to the
    default duration.
-   `COUNTDOWN`: a countdown is scheduled for a specific absolute server
    timestamp.
-   `GO`: the target time has been reached; clients show the full-screen
    green `GO!` screen for five seconds.
-   After five seconds in `GO`, return to `LOBBY`, reset the display to
    10 seconds, and preserve participant/readiness state.

Transitions:

``` text
LOBBY -- host starts (all connected players Ready) --> COUNTDOWN
COUNTDOWN -- host cancels --> LOBBY
COUNTDOWN -- target time reached --> GO
GO -- 5 seconds elapsed --> LOBBY
LOBBY -- host starts again --> COUNTDOWN
```

Rules: - Only the server decides state transitions. - The server stores
`startAt` as an absolute Unix timestamp with millisecond precision (or
an equivalent UTC instant). - Start time must be sufficiently in the
future to allow the start event to reach clients before the deadline.
Use a configurable scheduling lead; recommended initial value:
1500--2500 ms in addition to the chosen countdown duration if the
countdown is interpreted as the displayed duration. The UI must show the
countdown relative to the same `startAt`. - When the host presses Start,
the server validates the host, room state, readiness, and duration; it
then broadcasts the authoritative countdown state. - When the host
cancels, the server immediately transitions to `LOBBY`, invalidates the
active round ID, and broadcasts cancellation/reset state. - Cancellation
and completion must be idempotent. Delayed or duplicate messages for an
old round must not revive an old countdown. - Assign a monotonically
increasing `roundId` or unique round token to each countdown. Clients
ignore events from stale rounds. - On countdown completion, broadcast
`GO` with the current `roundId` and target timestamp. Clients should
show `GO!` based on the authoritative target time, not based on when the
WebSocket message happens to arrive. - The server transitions back to
`LOBBY` five seconds after the target timestamp. Clients should also
derive the end of the green screen from the authoritative timestamps, so
late messages do not extend the display inconsistently. - After
returning to `LOBBY`, reset the displayed countdown to 10 seconds.

### Timing and correctness note

This system aligns each browser's *scheduled signal time*; it cannot
guarantee that humans physically press the in-game button at the same
instant. Human reaction time, device scheduling, browser throttling,
display refresh, input latency, and game behavior all contribute
additional error.

## 8. Clock synchronization and latency measurement

Do not use the YouTube stream or Discord audio/video as the timing
source.

Each connected client periodically performs a WebSocket request/response
time sample: - Client records local monotonic time before sending
(`performance.now()`). - Server records receive and send timestamps in
its wall-clock time domain. - Client records local monotonic time on
response. - Use multiple samples and prefer low-RTT samples to estimate
the offset between client and server time. - Expose RTT as a measured
network round-trip time, not as "input latency". - Display latency in
milliseconds, rounded to whole milliseconds. - Display an explicit
syncing/unstable state when there are not enough samples or measurements
vary excessively. - The UI may show an estimated synchronization
accuracy only if the implementation can calculate it honestly; do not
present a guessed value as a guarantee.

Use `performance.now()` for local elapsed-time measurement and animation
loops. Avoid decrementing a counter once per second, as that accumulates
drift. Instead, compute remaining time on each animation frame from the
estimated server time and `startAt`.

The server should use a consistent server clock for room timestamps.
Clients must update their server-time offset from repeated samples and
calculate:

`remainingMs = startAtServerMs - estimatedServerNowMs`

Clamp displayed remaining time at zero. Format as `MM:SS.mmm` for the
main countdown, with responsive sizing on small screens.

WebSocket latency measurements are estimates. Network path asymmetry
means the offset calculation is not exact; avoid promising
sub-millisecond synchronization.

## 9. WebSocket protocol

Use JSON messages with a `type` discriminator. Validate every client
message server-side and reject unknown message types or invalid
payloads.

Suggested client-to-server messages:

-   `join_room`: room code, nickname, optional reconnect token.
-   `set_ready`: boolean.
-   `start_countdown`: requested duration in milliseconds (optional if
    fixed at 10 seconds).
-   `cancel_countdown`: round ID.
-   `time_sync`: client send timestamp or sample identifier.
-   `ping`: optional keepalive if needed.

Suggested server-to-client messages:

-   `room_state`: full authoritative room snapshot, including room ID,
    room state, default duration, `startAt`, `goUntil`, round ID, host
    participant ID, and participants.
-   `participant_joined`
-   `participant_updated`
-   `participant_left`
-   `countdown_started`
-   `countdown_cancelled`
-   `go`
-   `countdown_reset`
-   `time_sync_result`
-   `error`

Prefer a full `room_state` snapshot on join/reconnect and after state
recovery. Incremental participant events may be used during normal
operation. Every state-changing event should include room ID and, where
relevant, round ID and server timestamp.

The exact message structs and error codes should be documented in code.
Do not trust a client-provided host flag, participant ID, readiness
state for another participant, `startAt`, or room state. The server
computes authoritative state and timestamps.

## 10. UI and visual design

Design principle: **Minimal UI, Maximum Signal**.

### Visual style

-   Dark near-black / graphite base.
-   Restrained neon cyan as the primary accent.
-   Purple/blue accents for secondary decoration.
-   Green reserved for Ready/success and especially the full-screen
    `GO!` moment.
-   Red reserved for cancel/error.
-   Clean typography, large tabular timer digits, subtle glow and
    restrained transitions.
-   Avoid visual clutter, excessive cards, decorative graphs, and
    persistent technical metrics that players do not need.

### Player screen

Show only: - Room code. - Nickname. - Large countdown. - Ready/Not Ready
control when the room is in `LOBBY`. - Compact participant list with
nickname, readiness, and RTT. - A concise connection/synchronization
status when needed.

During `COUNTDOWN`, prioritize the timer and disable readiness changes.
During `GO`, replace the normal UI with a full-screen green state and a
large `GO!`. The green state lasts five seconds, then the normal lobby
UI returns.

### Host screen

Show: - Room code and copy/share link action. - Participant list with
readiness and RTT. - Start button when the room is eligible. - Cancel
button during countdown. - A minimal duration control only if included;
default 10 seconds.

On mobile, controls must be touch-friendly and avoid requiring hover. On
desktop, the host view may use the available width but must remain
visually restrained. Provide a separate minimal OBS-friendly view only
if it can be implemented without complicating the core MVP.

### Responsive and accessibility requirements

-   Mobile-first CSS; support narrow screens around 320 px wide and
    modern desktop screens.
-   Use Flexbox/Grid and CSS `clamp()` for responsive sizing.
-   Respect `prefers-reduced-motion`.
-   Do not rely on color alone to communicate Ready, Not Ready,
    disconnected, or error states; include text or an icon.
-   Maintain readable contrast and sufficiently large touch targets.
-   The `GO!` state must remain legible in portrait orientation and on
    desktop.
-   Prevent accidental page scroll/selection during the full-screen
    `GO!` state without breaking normal accessibility.

## 11. Disconnects, reconnection, and stale state

-   Detect WebSocket close and show a visible disconnected/reconnecting
    state.
-   Attempt reconnect with bounded exponential backoff and jitter.
-   On reconnect, send a join/reconnect request and request a full
    `room_state` snapshot.
-   A reconnecting client must not assume the previous countdown is
    still active until authoritative state is received.
-   If the client misses the countdown-start event, it must recover from
    `room_state` and compute remaining time from `startAt`.
-   If the client receives `GO` late, compare current estimated server
    time to `goUntil`; if the five-second display window has expired, do
    not show a stale `GO!` screen.
-   Host disconnect during countdown: cancel the countdown server-side
    and return the room to `LOBBY`; show a clear host-disconnected
    message. This is safer than continuing a countdown without the host.
-   Do not allow a client to start or cancel a countdown by replaying
    messages after a round has ended.

## 12. Security and robustness

-   Serve over HTTPS in production; use secure WebSocket (`wss://`).
-   Validate room codes, nicknames, message sizes, and all state
    transitions on the server.
-   Set read/write deadlines and message size limits for WebSocket
    connections.
-   Apply reasonable rate limits to room creation, joining, and
    time-sync requests.
-   Use cryptographically secure room ID generation.
-   Escape/render user-controlled nicknames as text.
-   Do not expose stack traces or internal errors to clients.
-   Ensure a client can only mutate its own readiness and only the host
    can control the countdown.
-   In-memory rooms are ephemeral and disappear on server restart. This
    is accepted for the MVP and must be documented.

## 13. Testing requirements

Implement tests for at least:

### Unit tests

-   Room ID format and uniqueness collision retry.
-   Nickname validation and normalization.
-   Room state transitions.
-   Start rejected when any connected participant is Not Ready.
-   Start rejected for non-host and while another countdown is active.
-   Cancel accepted only for host and active round.
-   Old/stale round IDs ignored.
-   `GO` transitions back to `LOBBY` after five seconds.
-   Readiness is preserved between rounds.
-   Host disconnect during countdown cancels the round.

### Integration / manual verification

-   Multiple browser clients can join one room and see participant
    updates.
-   Readiness changes propagate to everyone.
-   Start and cancel propagate correctly.
-   All clients derive countdown and `GO` timing from the same
    authoritative timestamps.
-   Reconnect restores the correct room state.
-   Mobile layout works in portrait and landscape.
-   `GO!` state appears for five seconds and then returns to the lobby
    with the timer reset to 10 seconds.
-   No React or other frontend framework is introduced.

Use an injectable clock/timer abstraction for backend lifecycle tests so
tests do not need to sleep for real countdowns.

## 14. Acceptance criteria

The MVP is complete when:

1.  A host can create a room with a unique six-character uppercase
    alphanumeric code.
2.  The host can share a room URL.
3.  Players can join, set a nickname, and see all participants.
4.  Each participant's Ready/Not Ready state and measured RTT are
    visible to the room.
5.  The host can start a 10-second countdown only when all connected
    participants are Ready.
6.  The host can cancel a running countdown, returning all clients to
    the lobby state.
7.  At the scheduled target time, every connected client transitions to
    a full-screen green `GO!` state.
8.  `GO!` remains visible for five seconds, then all clients return to
    the lobby and the timer resets to 10 seconds.
9.  The host can repeat the process without recreating the room.
10. A late join/reconnect receives the authoritative current room state.
11. The layout is modern, minimalist, high-tech, mobile-first, and
    usable on small screens.
12. The app accurately labels RTT as network round-trip time and does
    not claim it measures human or game input latency.

## 15. Out of scope

-   Automatic keyboard/mouse input or game integration.
-   YouTube/Discord API integration.
-   Persistent database storage or room history.
-   Accounts, registration, and permanent profiles.
-   Voice chat, messaging, and spectator features.
-   Guaranteed deterministic or sub-millisecond physical input
    synchronization.
-   Multi-instance horizontal scaling or distributed room state.

## 16. Implementation instructions for Claude Code

1.  Inspect the repository and existing conventions before changing
    files.
2.  Implement the smallest complete vertical slice: room
    creation/joining, WebSocket room state, readiness, countdown,
    cancel, GO state, and reset.
3.  Keep business rules in Go; treat the browser as an untrusted client.
4.  Keep frontend code in vanilla JavaScript and CSS.
5.  Add automated tests for state transitions and validation before
    polishing animations.
6.  Run formatting, tests, and available static checks. Fix failures
    introduced by the implementation.
7.  Update README with local run instructions, configuration, WebSocket
    protocol summary, and known limitations.
8.  Do not claim tests passed unless they were actually run.

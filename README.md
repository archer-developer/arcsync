# Sync Start

**Synchronized multiplayer countdown for cooperative games.**

One host starts the countdown; all players see the same timer and click their in-game start button the moment **GO!** appears.

---

## Screenshots

| Landing | Lobby (host) | All ready |
|---------|-------------|-----------|
| ![Landing](docs/screenshots/landing.jpg) | ![Lobby](docs/screenshots/room-lobby.jpg) | ![Ready](docs/screenshots/room-ready.jpg) |

| Countdown | GO! |
|-----------|-----|
| ![Countdown](docs/screenshots/countdown.jpg) | ![GO!](docs/screenshots/go.jpg) |

---

## How it works

1. **Host** enters a nickname and clicks **Create Room** → gets a shareable 6-character code.
2. **Players** open the link or type the code, enter a nickname, and join.
3. Everyone clicks **I'm Ready**. The host's **Start Countdown** button lights up when all are ready.
4. The host starts the countdown. A 10-second timer counts down for everyone simultaneously.
5. At zero, a full-screen green **GO!** signal appears for 5 seconds.
6. The host can start another round without recreating the room.

> **Note:** This synchronises the *signal time*, not physical inputs. Human reaction time, network jitter, display latency, and game scheduling all contribute additional variance.

---

## Running locally

**Requirements:** Go 1.21+

```bash
git clone https://github.com/yourorg/arcsync
cd arcsync
go run ./cmd/server          # listens on :8080 by default
```

Open <http://localhost:8080> in your browser.

**Custom port:**

```bash
go run ./cmd/server -addr :9090
```

---

## Configuration

All configuration is via command-line flags:

| Flag    | Default | Description           |
|---------|---------|-----------------------|
| `-addr` | `:8080` | HTTP listen address   |

Room state is **in-memory** — rooms disappear on server restart. This is by design for the MVP.

---

## Building for production

```bash
# Linux binary (for server deployment)
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o arcsync ./cmd/server

# Or with Docker
docker build -t arcsync .
docker run -p 8080:8080 arcsync
```

---

## Development

### Run tests

```bash
# Go unit tests
go test ./...

# JavaScript tests
npm test

# All linters
npm run lint           # ESLint + Stylelint
golangci-lint run      # Go linters
```

### Project structure

```
cmd/server/main.go          HTTP server, WebSocket upgrade, static files
internal/
  room/                     Room state machine, participant management, registry
  realtime/                 WebSocket hub, client pump goroutines, message types
  syncclock/                Injectable clock interface (real + test-friendly)
web/
  index.html                Single HTML page (SPA — all screens in one file)
  css/styles.css            Dark high-tech styling, CSS variables, responsive
  js/
    timer.js                Clock sync, requestAnimationFrame countdown, formatTime
    socket.js               WebSocket client, auto-reconnect with exponential backoff
    app.js                  Room state, routing, DOM updates
    timer.test.js           Jest tests for timer utilities
```

---

## WebSocket protocol summary

**Client → Server**

| Type                | Purpose                                    |
|---------------------|--------------------------------------------|
| `join_room`         | Join or reconnect to a room                |
| `set_ready`         | Toggle ready state                         |
| `start_countdown`   | Host: start the countdown                  |
| `cancel_countdown`  | Host: cancel an active countdown           |
| `time_sync`         | Clock sync probe (sent every 2 seconds)    |

**Server → Client**

| Type                  | Purpose                                        |
|-----------------------|------------------------------------------------|
| `room_state`          | Full room snapshot (on join/reconnect)         |
| `participant_joined`  | New participant connected                      |
| `participant_updated` | Readiness or RTT changed                       |
| `participant_left`    | Participant disconnected                       |
| `countdown_started`   | Authoritative `startAt` and `goUntil` (Unix ms)|
| `countdown_cancelled` | Countdown cancelled by host or host disconnect |
| `go`                  | Countdown reached zero                         |
| `countdown_reset`     | GO window expired, back to lobby               |
| `time_sync_result`    | Echo of sync probe with server timestamps      |
| `error`               | Validation or state error                      |

All timestamps are **Unix milliseconds**. Clients compute remaining time as `startAt - estimatedServerNow` on every animation frame.

---

## Latency display

The RTT shown next to each participant is the **WebSocket round-trip time** — the time from when the client sent a sync probe to when it received the response. It is **not** a measure of human reaction time or game input latency.

---

## Known limitations

- Rooms are in-memory and disappear on server restart.
- No horizontal scaling — all clients in a room must connect to the same process.
- No SSL termination built in — use a reverse proxy (Nginx, Caddy) for HTTPS in production.
- Clock synchronisation is based on RTT halving, which assumes symmetric network paths. Highly asymmetric paths will introduce a small offset error.

# ADR-001 — SYNC START: Changes from the Previous Version

**Status:** Proposed  
**Purpose:** This document records only the changes to the existing SYNC START ADR. All requirements and decisions not mentioned here remain unchanged.

## 1. Start scheduling: relative countdown → absolute start time

### Previous
The host started or cancelled a relative countdown (10 seconds by default).

### Updated
The host schedules a specific absolute date and time for the start. The room receives one shared `startAt` timestamp. The countdown shown to each participant is the remaining time until that timestamp; it must not restart when a message arrives.

- Interpret the selected time in the host's local time zone and convert it to an unambiguous timestamp (UTC epoch milliseconds) before sending it.
- Reject a start time that is already in the past or too close to reliably distribute to participants.
- The server remains authoritative for room state and validates host-only scheduling/cancellation.
- Every client uses the same `startAt`; do not apply a per-player ping-based shift to the actual start moment.
- Keep a unique `roundId` so cancellation or rescheduling invalidates stale timers and scheduled audio.
- On reaching `startAt`, show the existing `GO!` state for five seconds, then reset to the lobby state as before.

## 2. Clock-offset diagnostics

### Previous
Network latency was considered mainly as a possible input to start synchronization.

### Updated
Show a per-player estimate of system-clock offset relative to the host, alongside RTT (round-trip time), to help identify devices whose clocks may not be synchronized.

- Define the displayed offset as `player clock − host clock`: negative means the player's clock is behind; positive means it is ahead.
- Estimate offset using timestamp exchanges and RTT, rather than comparing timestamps from unrelated messages without accounting for transit time.
- Display the result as an estimate, not a guarantee. Network asymmetry and scheduling delays limit accuracy.
- Use configurable status thresholds (initial defaults: green under 50 ms, yellow 50–200 ms, red above 200 ms) and show unavailable/stale measurements distinctly.
- Do not claim to verify whether the OS is actually using NTP or another time-sync service. This feature only detects differences relative to the host.
- Clock offset is diagnostic information; it must not independently shift a player's `GO!` time. If a large offset is detected, warn the participant.

## 3. Sound signal for players looking at the game

### Previous
The main start cue was the visual `GO!` screen.

### Updated
Add an audible cue because participants may have the game in the foreground and may not be looking at the browser.

- Provide `Enable sound` (or equivalent explicit user gesture) and `Test sound` controls.
- Show a clear sound status, such as `Sound ready`, `Sound unavailable`, or `Sound needs activation`.
- Preload sound assets when a participant joins the room; decode them in advance when supported.
- Include short countdown cues at configurable final marks (default: 3, 2, 1) and a distinct `GO!` cue at `startAt`.
- The sound must be scheduled against the same absolute start deadline, not started only after a late JavaScript timer callback.
- Use Web Audio API scheduling where available. Prepare the audio context and buffers before the round; handle a suspended context or playback failure visibly.
- Cancel pending audio when the host cancels/reschedules the round, using `roundId` to prevent stale cues.
- Keep the visual `GO!` state as a fallback; sound activation must not be assumed successful merely because the audio file loaded.

## 4. Background-tab and platform limitations

### Previous
The design did not explicitly account for audio delivery when the browser is hidden.

### Updated
Document and handle browser/OS limitations explicitly:

- Preloading and scheduling audio in advance improves reliability but cannot guarantee exact playback while a tab is backgrounded, throttled, suspended, or the device is in a power-saving mode.
- Mobile browsers and operating systems may restrict background audio. Do not promise reliable background playback on every device.
- If the page detects that audio is unavailable or the audio context is suspended, show a warning before the round when possible.
- Use `Date.now()` to relate to the absolute wall-clock deadline and `performance.now()` for monotonic elapsed-time scheduling. Re-check the deadline after visibility changes or timer delays; do not rely on `setTimeout()` as the source of truth.
- The server's clock should be synchronized with a reliable time source (for example, NTP). Client OS clock synchronization is expected but is not guaranteed.

## 5. Acceptance criteria added

- All connected clients receive the same absolute `startAt` and count down to that instant.
- A late WebSocket message does not restart or extend the countdown.
- A cancelled or replaced round cannot trigger an old visual or audio cue.
- Each player sees a clock-offset estimate and RTT, with stale/unavailable states represented clearly.
- A large clock offset produces a warning but does not silently alter that player's start timestamp.
- Sound can be explicitly enabled and tested; assets are loaded and prepared before a round.
- Countdown and `GO!` cues are scheduled ahead of time where the browser permits it, and audio errors/suspension are surfaced to the user.
- Documentation states that exact simultaneous sound across browsers/devices is best-effort, not guaranteed.

---

**Unchanged:** All other decisions, features, UI requirements, room rules, technology choices, and protocol behavior from the previous ADR remain in force unless explicitly superseded above.

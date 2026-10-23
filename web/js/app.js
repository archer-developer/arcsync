/* global Timer, Sound, Socket, location, sessionStorage, document, fetch, Date */
'use strict';

// ---- State ----
var myParticipantID = null;
var myHostToken     = '';
var isHost          = false;
var roomState       = 'LOBBY';
var currentRoomID   = null;
var currentRoundID  = 0;
var startAt         = 0;
var goUntil         = 0;
var durationMS      = 0;
var START_DELAY_MS  = 10000; // start = host "now" + 10 s, sent as absolute UTC ms
var participants    = {};  // id → ParticipantView
var hostID          = null;
var goTimeout       = null;

// ---- Browser support check ----
function checkBrowserSupport() {
  var missing = [];
  if (typeof WebSocket === 'undefined') {
    missing.push('WebSockets — required for real-time room communication');
  }
  if (typeof performance === 'undefined' || typeof performance.now !== 'function') {
    missing.push('Performance API — required for accurate countdown timing');
  }
  if (typeof fetch === 'undefined') {
    missing.push('Fetch API — required for creating rooms');
  }
  return missing;
}

function showUnsupportedScreen(missing) {
  var list = el('missing-features');
  if (list) {
    missing.forEach(function (item) {
      var li = document.createElement('li');
      li.textContent = item;
      list.appendChild(li);
    });
  }
  document.querySelectorAll('.screen').forEach(function (s) {
    s.hidden = s.id !== 'screen-unsupported';
  });
}

// ---- Entry point ----
function init() {
  var missing = checkBrowserSupport();
  if (missing.length > 0) {
    showUnsupportedScreen(missing);
    return;
  }

  var path = location.pathname;
  if (path.startsWith('/room/')) {
    var rid = path.slice('/room/'.length).toUpperCase();
    if (/^[A-Z0-9]{6}$/.test(rid)) {
      currentRoomID = rid;
      roomExists(rid).then(function (exists) {
        if (exists === false) { leaveMissingRoom(rid); } else { showRoomJoin(rid); }
      });
    } else {
      leaveMissingRoom(rid);
    }
  } else {
    showLanding();
  }

  document.addEventListener('visibilitychange', function () {
    if (!document.hidden) {
      Sound.tryResume();
      updateSoundStatus();
    }
  });
}

// Resolves true/false, or null when the server cannot be reached.
function roomExists(rid) {
  return fetch('/api/rooms/' + rid, { method: 'GET', cache: 'no-store' })
    .then(function (res) {
      if (res.status === 404) { return false; }
      return res.ok ? true : null;
    })
    .catch(function () { return null; });
}

// The room is gone: forget its stored session and go back to the first screen with an error.
function leaveMissingRoom(rid) {
  Socket.disconnect();
  Timer.stopSync();
  ['host_token_', 'nickname_', 'pid_'].forEach(function (k) { sessionStorage.removeItem(k + rid); });
  sessionStorage.setItem('flash_error', 'Room ' + rid + ' not found or has expired.');
  location.replace('/');
}

// ---- Landing screen ----
function showLanding() {
  screen('landing');

  var flash = sessionStorage.getItem('flash_error');
  if (flash) {
    sessionStorage.removeItem('flash_error');
    showFieldError('join-code-error', flash);
  }

  el('btn-create').addEventListener('click', createRoom);

  el('btn-join-code').addEventListener('click', function () {
    var code = el('input-join-code').value.trim().toUpperCase();
    if (/^[A-Z0-9]{6}$/.test(code)) {
      location.href = '/room/' + code;
    } else {
      showFieldError('join-code-error', 'Enter a valid 6-character room code (A-Z, 0-9)');
    }
  });

  el('input-join-code').addEventListener('input', function () {
    clearFieldError('join-code-error');
    el('input-join-code').value = el('input-join-code').value.toUpperCase();
  });
  el('input-join-code').addEventListener('keydown', function (e) {
    if (e.key === 'Enter') { el('btn-join-code').click(); }
  });
  el('input-host-nick').addEventListener('keydown', function (e) {
    if (e.key === 'Enter') { createRoom(); }
  });
}

async function createRoom() {
  var nick = el('input-host-nick').value.trim();
  if (!validateNickname(nick, 'host-nick-error')) { return; }

  el('btn-create').disabled = true;
  try {
    var res = await fetch('/api/rooms', { method: 'POST' });
    if (!res.ok) { throw new Error('Server error'); }
    var data = await res.json();
    sessionStorage.setItem('host_token_' + data.room_id, data.host_token);
    sessionStorage.setItem('nickname_' + data.room_id, nick);
    location.href = '/room/' + data.room_id;
  } catch {
    el('btn-create').disabled = false;
    showFieldError('host-nick-error', 'Could not create room. Please try again.');
  }
}

// ---- Room join ----
function showRoomJoin(rid) {
  var storedNick = sessionStorage.getItem('nickname_' + rid);
  if (storedNick) {
    connectToRoom(rid, storedNick);
    return;
  }
  screen('join');
  el('join-room-code-display').textContent = rid;
  el('btn-join-room').addEventListener('click', function () {
    var nick = el('input-player-nick').value.trim();
    if (!validateNickname(nick, 'player-nick-error')) { return; }
    sessionStorage.setItem('nickname_' + rid, nick);
    connectToRoom(rid, nick);
  });
  el('input-player-nick').addEventListener('keydown', function (e) {
    if (e.key === 'Enter') { el('btn-join-room').click(); }
  });
}

function connectToRoom(rid, nick) {
  screen('room');
  el('room-code-display').textContent = rid;

  myHostToken = sessionStorage.getItem('host_token_' + rid) || '';
  var myNick  = nick;

  Socket.onConnected(function () {
    setStatus('connected');
    var reconnectToken = sessionStorage.getItem('pid_' + rid) || '';
    Socket.send({
      type:            'join_room',
      room_id:         rid,
      nickname:        myNick,
      host_token:      myHostToken,
      reconnect_token: reconnectToken,
    });
    Timer.startSync(Socket.send.bind(Socket));
  });

  Socket.onDisconnected(function () {
    setStatus('disconnected');
    Timer.stopSync();
    // A dropped or refused connection may mean the room no longer exists.
    roomExists(rid).then(function (exists) {
      if (exists === false) { leaveMissingRoom(rid); }
    });
  });

  Socket.on('room_state',          handleRoomState);
  Socket.on('participant_joined',  handleParticipantJoined);
  Socket.on('participant_updated', handleParticipantUpdated);
  Socket.on('participant_left',    handleParticipantLeft);
  Socket.on('countdown_started',   handleCountdownStarted);
  Socket.on('countdown_cancelled', handleCountdownCancelled);
  Socket.on('go',                  handleGo);
  Socket.on('countdown_reset',     handleCountdownReset);
  Socket.on('time_sync_result',    handleTimeSyncResult);
  Socket.on('error',               handleServerError);

  Socket.connect(rid);
}

// ---- WebSocket handlers ----
function handleRoomState(msg) {
  hostID         = msg.host_id;
  roomState      = msg.state;
  durationMS     = msg.duration_ms;
  startAt        = msg.start_at;
  goUntil        = msg.go_until;
  currentRoundID = msg.round_id;
  participants   = {};
  (msg.participants || []).forEach(function (p) { participants[p.id] = p; });

  // The server tells us who we are and issues a secret token for resuming.
  myParticipantID = msg.you;
  if (msg.reconnect_token) {
    sessionStorage.setItem('pid_' + currentRoomID, msg.reconnect_token);
  }

  isHost = myHostToken !== '' && msg.host_id === myParticipantID;

  renderRoom();
  applyRoomState();
}

function handleParticipantJoined(msg) {
  participants[msg.participant.id] = msg.participant;
  renderParticipants();
}

function handleParticipantUpdated(msg) {
  participants[msg.participant.id] = msg.participant;
  renderParticipants();
  if (msg.participant.id === myParticipantID) {
    updateReadyButton(msg.participant.ready);
  }
}

function handleParticipantLeft(msg) {
  if (participants[msg.participant_id]) {
    participants[msg.participant_id].connected = false;
    renderParticipants();
  }
}

function handleCountdownStarted(msg) {
  roomState      = 'COUNTDOWN';
  currentRoundID = msg.round_id;
  startAt        = msg.start_at;
  goUntil        = msg.go_until;
  durationMS     = msg.duration_ms;
  renderControls();
  renderCountdown(Timer.calculateRemainingMs(startAt));

  Sound.scheduleRound(startAt, currentRoundID);

  Timer.startTimerLoop({
    startAt:  startAt,
    goUntil:  goUntil,
    onTick:   function (ms) { renderCountdown(ms); },
    onGo:     function () { showGoState(); },
    onLobby:  function () { returnToLobby(); },
  });
}

function handleCountdownCancelled(msg) {
  if (msg.round_id !== currentRoundID) { return; }
  Sound.cancelRound(msg.round_id);
  roomState = 'LOBBY';
  Timer.stopTimerLoop();
  renderControls();
  renderCountdown(0);
  setCountdownLabel('Ready');
}

function handleGo(msg) {
  if (msg.round_id !== currentRoundID) { return; }
  var serverNow = Timer.estimatedServerNow();
  if (msg.go_until > 0 && serverNow >= msg.go_until) { return; } // late, skip
  goUntil = msg.go_until;
  showGoState();
}

function handleCountdownReset(msg) {
  Sound.cancelRound(msg.round_id);
  roomState  = 'LOBBY';
  durationMS = 0;
  Timer.stopTimerLoop();
  hideGoOverlay();
  renderControls();
  renderCountdown(0);
  setCountdownLabel('Ready');
}

function handleTimeSyncResult(msg) {
  Timer.processTimeSyncResult(msg);
  // RTT is now sent by the client in subsequent time_sync probes and updated server-side.
  // Update local participant view optimistically for immediate UI feedback.
  var rtt = Timer.currentRTT();
  if (rtt !== null && myParticipantID && participants[myParticipantID]) {
    participants[myParticipantID].rtt_ms = rtt;
    renderParticipants();
  }
}

function handleServerError(msg) {
  console.warn('[sync-start error]', msg.code, msg.message);
  if (msg.code === 'start_failed') {
    showFieldError('start-time-error', msg.message);
  }
}

// ---- Rendering ----
function applyRoomState() {
  renderControls();
  if (roomState === 'COUNTDOWN') {
    setCountdownLabel('Until start');
    renderCountdown(Timer.calculateRemainingMs(startAt));
    Sound.scheduleRound(startAt, currentRoundID);
    Timer.startTimerLoop({
      startAt:  startAt,
      goUntil:  goUntil,
      onTick:   function (ms) { renderCountdown(ms); },
      onGo:     function () { showGoState(); },
      onLobby:  function () { returnToLobby(); },
    });
  } else if (roomState === 'GO') {
    showGoState();
  } else {
    Sound.cancelRound(currentRoundID);
    Timer.stopTimerLoop();
    setCountdownLabel('Ready');
    renderCountdown(0);
    hideGoOverlay();
  }
}

function renderRoom() {
  el('host-badge').hidden = !isHost;
  renderParticipants();
}

function renderControls() {
  var inCountdown = roomState === 'COUNTDOWN';
  var inLobby     = roomState === 'LOBBY';

  el('host-controls').style.display   = isHost ? 'flex' : 'none';
  el('player-controls').style.display = !isHost ? 'flex' : 'none';

  if (isHost) {
    var sched = el('schedule-section');
    if (sched)  { sched.style.display  = inLobby ? 'flex' : 'none'; }
    el('btn-cancel').style.display = inCountdown ? 'flex' : 'none';
    el('btn-ready').style.display  = inLobby ? 'flex' : 'none';
    if (inLobby) {
      updateStartButton();
    }
  } else {
    el('btn-ready-player').disabled = !inLobby;
  }
  var me = participants[myParticipantID];
  updateReadyButton(me ? me.ready : false);
}

function renderParticipants() {
  var list = el('participant-list');
  if (!list) { return; }
  list.innerHTML = '';

  var sorted = Object.keys(participants).map(function (id) {
    return participants[id];
  }).sort(function (a, b) {
    if (a.id === hostID) { return -1; }
    if (b.id === hostID) { return  1; }
    return a.nickname.localeCompare(b.nickname);
  });

  // Compute host's clock offset for relative display
  var hostOffsetMS = null;
  if (hostID && hasOffset(participants[hostID])) {
    hostOffsetMS = participants[hostID].clock_offset_ms;
  }

  sorted.forEach(function (p) {
    var item = document.createElement('div');
    item.className = 'participant-item' + (p.connected ? '' : ' disconnected');
    item.setAttribute('role', 'listitem');

    var avatar = document.createElement('span');
    avatar.className = 'avatar ' + avatarClass(p);
    avatar.setAttribute('aria-hidden', 'true');
    avatar.textContent = p.nickname.charAt(0).toUpperCase();

    var info = document.createElement('div');
    info.className = 'participant-info';

    var name = document.createElement('span');
    name.className = 'participant-name';
    name.textContent = p.nickname;

    if (p.id === myParticipantID) {
      var you = document.createElement('span');
      you.className = 'you-label';
      you.textContent = ' (you)';
      name.appendChild(you);
    }
    if (p.id === hostID) {
      var badge = document.createElement('span');
      badge.className = 'host-label';
      badge.textContent = ' host';
      name.appendChild(badge);
    }

    var stats = document.createElement('div');
    stats.className = 'participant-stats';

    var rtt = document.createElement('span');
    rtt.className = 'participant-rtt';
    rtt.textContent = p.rtt_ms > 0 ? p.rtt_ms + ' ms' : '—';
    rtt.title = 'Round-trip time';

    var offsetEl = document.createElement('span');
    offsetEl.className = 'participant-offset ' + clockOffsetClass(p, hostOffsetMS);
    offsetEl.textContent = clockOffsetText(p, hostOffsetMS);
    offsetEl.title = 'Estimated clock offset vs host';

    stats.appendChild(rtt);
    stats.appendChild(offsetEl);

    var dot = document.createElement('span');
    dot.className = 'ready-dot ' + readyClass(p);
    dot.setAttribute('title', readyLabel(p));
    dot.setAttribute('aria-label', readyLabel(p));

    info.appendChild(name);
    item.appendChild(avatar);
    item.appendChild(info);
    item.appendChild(stats);
    item.appendChild(dot);
    list.appendChild(item);
  });

  // All-ready banner
  var connected = Object.keys(participants).map(function (id) { return participants[id]; })
                        .filter(function (p) { return p.connected; });
  var allReady  = connected.length > 0 && connected.every(function (p) { return p.ready; });
  var indicator = el('all-ready-indicator');
  if (indicator) {
    indicator.textContent = allReady ? '✓ All ready' : '';
    indicator.className   = 'all-ready' + (allReady ? ' visible' : '');
  }

  updateStartButton();
  checkClockOffsetWarnings();
}

// clock_offset_ms is omitted by the server until measured.
function hasOffset(p) { return p && typeof p.clock_offset_ms === 'number'; }

function clockOffsetText(p, hostOffsetMS) {
  if (!hasOffset(p))         { return '—'; }
  if (p.id === hostID)       { return 'host'; }
  if (hostOffsetMS === null) { return '?'; }
  var rel = p.clock_offset_ms - hostOffsetMS;
  return (rel >= 0 ? '+' : '') + rel + ' ms';
}

function clockOffsetClass(p, hostOffsetMS) {
  if (!hasOffset(p) || p.id === hostID || hostOffsetMS === null) {
    return 'offset-unknown';
  }
  var abs = Math.abs(p.clock_offset_ms - hostOffsetMS);
  if (abs < 50)  { return 'offset-good'; }
  if (abs < 200) { return 'offset-warn'; }
  return 'offset-bad';
}

function checkClockOffsetWarnings() {
  var warnEl = el('clock-offset-warning');
  if (!warnEl) { return; }
  var host = hostID ? participants[hostID] : null;
  var me   = participants[myParticipantID];
  if (!hasOffset(host) || !hasOffset(me)) { warnEl.hidden = true; return; }
  var rel = Math.abs(me.clock_offset_ms - host.clock_offset_ms);
  if (rel >= 200) {
    warnEl.textContent = 'Your clock may be out of sync with the host by ~' + rel + ' ms. Consider syncing your system clock.';
    warnEl.hidden = false;
  } else {
    warnEl.hidden = true;
  }
}

function updateStartButton() {
  var btn = el('btn-start');
  if (!btn) { return; }
  var connected = Object.keys(participants).map(function (id) { return participants[id]; })
                        .filter(function (p) { return p.connected; });
  var allReady  = connected.length > 0 && connected.every(function (p) { return p.ready; });
  btn.disabled  = !allReady;
  btn.title     = allReady ? '' : 'All participants must be ready';
}

function updateReadyButton(ready) {
  var ids = ['btn-ready', 'btn-ready-player'];
  ids.forEach(function (id) {
    var btn = el(id);
    if (!btn) { return; }
    btn.textContent = ready ? 'Not Ready' : "I'm Ready";
    btn.className   = ready ? 'btn btn-ghost btn-full' : 'btn btn-outline btn-full';
  });
}

function updateSoundStatus() {
  var status  = Sound.getStatus();
  var statusEl = el('sound-status');
  var testBtn  = el('btn-test-sound');
  var enableBtn = el('btn-enable-sound');
  if (!statusEl) { return; }

  var labels = {
    'off':              'Sound off',
    'ready':            'Sound ready',
    'needs-activation': 'Tap anywhere to enable sound',
    'unavailable':      'Sound unavailable',
  };
  statusEl.textContent = labels[status] || status;
  statusEl.className   = 'sound-status sound-' + status.replace('-', '');

  if (testBtn)  { testBtn.disabled  = status !== 'ready'; }
  if (enableBtn) { enableBtn.disabled = status === 'ready' || status === 'unavailable'; }
}

function renderCountdown(ms) {
  var timerEl = el('countdown-timer');
  if (timerEl) {
    timerEl.textContent = (roomState === 'LOBBY' && ms <= 0)
      ? Timer.formatTime(START_DELAY_MS)
      : Timer.formatTime(ms);
  }
  var arc = el('countdown-arc');
  if (arc) {
    if (durationMS > 0) {
      var pct    = Math.min(1, Math.max(0, ms / durationMS));
      var offset = Timer.getCircumference() * (1 - pct);
      arc.style.strokeDashoffset = offset;
    } else {
      arc.style.strokeDashoffset = 0; // full circle when idle
    }
  }
}

function setCountdownLabel(text) {
  var lbl = el('countdown-label');
  if (lbl) { lbl.textContent = text; }
}

function showGoState() {
  var overlay = el('overlay-go');
  if (!overlay) { return; }

  clearTimeout(goTimeout);
  overlay.hidden = false;
  document.body.classList.add('overlay-active');

  var remaining = goUntil > 0
    ? Math.max(0, goUntil - Timer.estimatedServerNow())
    : 5000;

  if (goUntil > 0 && Timer.estimatedServerNow() >= goUntil) {
    hideGoOverlay();
    return;
  }

  goTimeout = setTimeout(function () {
    hideGoOverlay();
    returnToLobby();
  }, remaining);
}

function hideGoOverlay() {
  clearTimeout(goTimeout);
  var overlay = el('overlay-go');
  if (overlay) { overlay.hidden = true; }
  document.body.classList.remove('overlay-active');
}

function returnToLobby() {
  roomState = 'LOBBY';
  hideGoOverlay();
  Timer.stopTimerLoop();
  renderControls();
  renderCountdown(0);
  setCountdownLabel('Ready');
}

// ---- Control wiring (called once when room screen is shown) ----
function setupControls() {
  function toggleReady() {
    var me = participants[myParticipantID];
    Socket.send({ type: 'set_ready', ready: !(me && me.ready) });
  }
  var btnReady       = el('btn-ready');
  var btnReadyPlayer = el('btn-ready-player');
  if (btnReady)       { btnReady.addEventListener('click', toggleReady); }
  if (btnReadyPlayer) { btnReadyPlayer.addEventListener('click', toggleReady); }

  el('btn-start').addEventListener('click', function () {
    clearFieldError('start-time-error');
    // Prefer the server-corrected clock so a skewed host clock cannot shift the start.
    var now = Timer.isSynced() ? Math.round(Timer.estimatedServerNow()) : Date.now();
    Socket.send({ type: 'start_countdown', start_at: now + START_DELAY_MS });
  });

  el('btn-cancel').addEventListener('click', function () {
    Socket.send({ type: 'cancel_countdown', round_id: currentRoundID });
  });

  el('btn-enable-sound').addEventListener('click', function () {
    Sound.init();
    updateSoundStatus();
  });

  el('btn-test-sound').addEventListener('click', function () {
    Sound.test();
    updateSoundStatus();
  });

  autoEnableSound();
}

// Browsers only start audio after a user gesture, so try now and again on the
// first click / tap / key press anywhere in the room.
function autoEnableSound() {
  var events = ['click', 'touchend', 'keydown'];
  function attempt() {
    Sound.init();
    updateSoundStatus();
    if (Sound.getStatus() === 'ready') { detach(); }
  }
  function detach() {
    events.forEach(function (ev) { document.removeEventListener(ev, attempt, true); });
  }
  Sound.onStateChange(function () {
    updateSoundStatus();
    if (Sound.getStatus() === 'ready') { detach(); }
  });
  events.forEach(function (ev) { document.addEventListener(ev, attempt, true); });
  attempt();
}

// ---- Screens ----
function screen(name) {
  document.querySelectorAll('.screen').forEach(function (s) {
    s.hidden = s.dataset.screen !== name;
  });
  if (name === 'room') { setupControls(); }
}

// ---- Status ----
function setStatus(status) {
  var statusEl = el('connection-status');
  if (!statusEl) { return; }
  statusEl.dataset.status = status;
  statusEl.textContent = status === 'connected'   ? 'Connected'
                       : status === 'disconnected' ? 'Reconnecting…'
                       :                             status;
}

// ---- Helpers ----
function el(id)  { return document.getElementById(id); }

function avatarClass(p) {
  if (!p.connected) { return 'avatar-disconnected'; }
  if (p.id === hostID) { return 'avatar-host'; }
  var colors = ['cyan', 'purple', 'green', 'orange', 'pink'];
  var idx = 0;
  for (var i = 0; i < p.id.length; i++) { idx = (idx + p.id.charCodeAt(i)) % colors.length; }
  return 'avatar-' + colors[idx];
}

function readyClass(p) {
  if (!p.connected) { return 'dot-disconnected'; }
  return p.ready ? 'dot-ready' : 'dot-waiting';
}

function readyLabel(p) {
  if (!p.connected) { return 'Disconnected'; }
  return p.ready ? 'Ready' : 'Not ready';
}

function validateNickname(nick, errorID) {
  if (!nick) {
    showFieldError(errorID, 'Nickname is required');
    return false;
  }
  if (Array.from(nick).length > 20) {
    showFieldError(errorID, 'Nickname must be 20 characters or fewer');
    return false;
  }
  clearFieldError(errorID);
  return true;
}

function showFieldError(id, msg) {
  var errEl = el(id);
  if (errEl) { errEl.textContent = msg; errEl.hidden = false; }
}

function clearFieldError(id) {
  var errEl = el(id);
  if (errEl) { errEl.textContent = ''; errEl.hidden = true; }
}

document.addEventListener('DOMContentLoaded', init);

/* global performance, requestAnimationFrame, cancelAnimationFrame */
'use strict';

var Timer = (function () {
  var SYNC_SAMPLES  = 5;
  var SYNC_INTERVAL = 2000;
  var SYNC_MIN_VALID = 3;
  var ARC_CIRCUMFERENCE = 2 * Math.PI * 120; // matches SVG r=120

  var samples     = [];
  var clockOffset = 0; // performance.now() + clockOffset = Unix ms
  var synced      = false;
  var syncTimer   = null;
  var rafHandle   = null;
  var timerState  = null;

  function estimatedServerNow() {
    return performance.now() + clockOffset;
  }

  function processTimeSyncResult(msg) {
    var clientRecvTs = performance.now();
    var rtt          = clientRecvTs - msg.client_send_ts;
    var serverMid    = (msg.server_recv_ts + msg.server_send_ts) / 2;
    var clientMid    = (msg.client_send_ts + clientRecvTs) / 2;
    var offset       = serverMid - clientMid;

    samples.push({ rtt: rtt, offset: offset });
    if (samples.length > SYNC_SAMPLES) { samples.shift(); }

    // Median of the best half by RTT
    var sorted = samples.slice().sort(function (a, b) { return a.rtt - b.rtt; });
    var best   = sorted.slice(0, Math.ceil(sorted.length / 2));
    var offsets = best.map(function (s) { return s.offset; })
                      .sort(function (a, b) { return a - b; });
    clockOffset = offsets[Math.floor(offsets.length / 2)];
    synced = samples.length >= SYNC_MIN_VALID;
  }

  function currentRTT() {
    if (!samples.length) { return null; }
    return Math.round(samples[samples.length - 1].rtt);
  }

  function isSynced() { return synced; }

  function currentClockOffset() { return clockOffset; }

  function startSync(sendFn) {
    stopSync();
    function probe() {
      sendFn({
        type:              'time_sync',
        client_send_ts:    performance.now(),
        client_now_unix_ms: Date.now(),
        rtt_ms:            currentRTT() || 0,
      });
    }
    probe();
    syncTimer = setInterval(probe, SYNC_INTERVAL);
  }

  function stopSync() {
    if (syncTimer !== null) { clearInterval(syncTimer); syncTimer = null; }
    samples     = [];
    synced      = false;
    clockOffset = 0;
  }

  /** Format milliseconds as seconds with tenths ("10.0"); "M:SS.d" from one minute up. */
  function formatTime(ms) {
    if (ms <= 0) { return '0.0'; }
    var ds  = Math.floor(ms / 100);
    var sec = Math.floor(ds / 10);
    var frac = ds % 10;
    if (sec < 60) { return sec + '.' + frac; }
    return Math.floor(sec / 60) + ':' + pad2(sec % 60) + '.' + frac;
  }

  function calculateRemainingMs(startAtUnixMs) {
    return Math.max(0, startAtUnixMs - estimatedServerNow());
  }

  function getCircumference() { return ARC_CIRCUMFERENCE; }

  // --- Animation loop ---

  function startTimerLoop(state) {
    timerState = state;
    if (rafHandle === null) {
      rafHandle = requestAnimationFrame(tick);
    }
  }

  function stopTimerLoop() {
    if (rafHandle !== null) { cancelAnimationFrame(rafHandle); rafHandle = null; }
    timerState = null;
  }

  function tick() {
    rafHandle = requestAnimationFrame(tick);
    if (!timerState) { return; }

    var now     = estimatedServerNow();
    var st      = timerState.startAt;
    var gu      = timerState.goUntil;
    var onTick  = timerState.onTick;
    var onGo    = timerState.onGo;
    var onLobby = timerState.onLobby;

    if (gu > 0 && now >= gu) {
      onLobby && onLobby();
      stopTimerLoop();
      return;
    }
    if (now >= st) {
      onGo && onGo();
      return;
    }
    onTick && onTick(st - now);
  }

  // --- Helpers ---
  function pad2(n) { return n < 10 ? '0' + n : String(n); }

  return {
    estimatedServerNow:    estimatedServerNow,
    processTimeSyncResult: processTimeSyncResult,
    currentRTT:            currentRTT,
    currentClockOffset:    currentClockOffset,
    isSynced:              isSynced,
    startSync:             startSync,
    stopSync:              stopSync,
    formatTime:            formatTime,
    calculateRemainingMs:  calculateRemainingMs,
    getCircumference:      getCircumference,
    startTimerLoop:        startTimerLoop,
    stopTimerLoop:         stopTimerLoop,
    // Test seams
    _setSamples: function (s) { samples = s; },
    _setOffset:  function (v) { clockOffset = v; },
    _getSamples: function () { return samples; },
    _getOffset:  function () { return clockOffset; },
  };
})();

if (typeof module !== 'undefined' && module.exports) {
  module.exports = Timer;
}

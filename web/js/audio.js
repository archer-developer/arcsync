/* global window */
/* exported Sound */
'use strict';

var Sound = (function () {
  var ctx = null;
  var scheduledSources = {}; // roundId → [OscillatorNode]
  var stateCb = null;

  // Create a synthetic beep at a given AudioContext time.
  function createBeep(freq, startTime, duration, volume) {
    if (!ctx) { return null; }
    var osc  = ctx.createOscillator();
    var gain = ctx.createGain();
    osc.connect(gain);
    gain.connect(ctx.destination);
    osc.type = 'sine';
    osc.frequency.value = freq;
    gain.gain.setValueAtTime(volume || 0.25, startTime);
    gain.gain.exponentialRampToValueAtTime(0.001, startTime + duration);
    osc.start(startTime);
    osc.stop(startTime + duration + 0.01);
    return osc;
  }

  // Convert a wall-clock Unix-ms timestamp to AudioContext time.
  function toAudioTime(wallMs) {
    return ctx.currentTime + (wallMs - Date.now()) / 1000;
  }

  function scheduleRound(startAtMs, roundId) {
    if (!ctx || ctx.state === 'closed') { return; }
    cancelRound(roundId);
    var sources = [];

    // Countdown beeps: 3 s, 2 s, 1 s before startAt
    var freqs   = [440, 550, 660];
    var offsets = [3000, 2000, 1000];
    for (var i = 0; i < offsets.length; i++) {
      var t = toAudioTime(startAtMs - offsets[i]);
      if (t > ctx.currentTime + 0.05) {
        var src = createBeep(freqs[i], t, 0.12, 0.2);
        if (src) { sources.push(src); }
      }
    }

    // GO! sound: brighter, slightly longer
    var goT = toAudioTime(startAtMs);
    if (goT > ctx.currentTime + 0.05) {
      var goSrc = createBeep(880, goT, 0.4, 0.35);
      if (goSrc) { sources.push(goSrc); }
    }

    scheduledSources[roundId] = sources;
  }

  function cancelRound(roundId) {
    var srcs = scheduledSources[roundId];
    if (!srcs) { return; }
    srcs.forEach(function (src) {
      try { src.stop(); } catch { /* source may already be stopped */ }
    });
    delete scheduledSources[roundId];
  }

  // Call from a user-gesture handler to create / resume the AudioContext.
  function init() {
    if (ctx) {
      if (ctx.state === 'suspended') { ctx.resume().catch(function () {}); }
      return ctx.state !== 'closed';
    }
    try {
      var AC = window.AudioContext || window.webkitAudioContext;
      if (!AC) { return false; }
      ctx = new AC();
      ctx.onstatechange = function () { if (stateCb) { stateCb(); } };
      if (ctx.state === 'suspended') { ctx.resume().catch(function () {}); }
      return true;
    } catch {
      return false;
    }
  }

  function test() {
    if (!ctx) { return; }
    if (ctx.state === 'suspended') { ctx.resume().catch(function () {}); }
    createBeep(880, ctx.currentTime + 0.05, 0.3, 0.3);
  }

  // 'off' | 'ready' | 'needs-activation' | 'unavailable'
  function getStatus() {
    if (!ctx)                      { return 'off'; }
    if (ctx.state === 'suspended') { return 'needs-activation'; }
    if (ctx.state === 'closed')    { return 'unavailable'; }
    return 'ready';
  }

  // Try to resume a suspended context (e.g. after tab becomes visible).
  function tryResume() {
    if (ctx && ctx.state === 'suspended') {
      ctx.resume().catch(function () {});
    }
  }

  function onStateChange(fn) { stateCb = fn; }

  return {
    init:          init,
    onStateChange: onStateChange,
    test:          test,
    getStatus:     getStatus,
    scheduleRound: scheduleRound,
    cancelRound:   cancelRound,
    tryResume:     tryResume,
  };
})();

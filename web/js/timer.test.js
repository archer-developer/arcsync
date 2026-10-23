'use strict';

// Stub browser APIs for Node.js test environment
global.performance = {
  _now: 0,
  now: function () { return global.performance._now; },
};
global.requestAnimationFrame = function (fn) { return setTimeout(fn, 16); };
global.cancelAnimationFrame  = function (id) { clearTimeout(id); };
global.setInterval  = setInterval;
global.clearInterval = clearInterval;

const Timer = require('./timer.js');

// Reset internal state between tests
beforeEach(function () {
  Timer._setSamples([]);
  Timer._setOffset(0);
});

// ---- formatTime ----

test('formatTime: zero', () => {
  expect(Timer.formatTime(0)).toBe('00:00.000');
});

test('formatTime: negative treated as zero', () => {
  expect(Timer.formatTime(-500)).toBe('00:00.000');
});

test('formatTime: 10 seconds', () => {
  expect(Timer.formatTime(10000)).toBe('00:10.000');
});

test('formatTime: 1 minute', () => {
  expect(Timer.formatTime(60000)).toBe('01:00.000');
});

test('formatTime: 1m 23s 456ms', () => {
  expect(Timer.formatTime(60000 + 23000 + 456)).toBe('01:23.456');
});

test('formatTime: millisecond precision', () => {
  expect(Timer.formatTime(9050)).toBe('00:09.050');
});

// ---- calculateRemainingMs ----

test('calculateRemainingMs: positive', () => {
  Timer._setOffset(1000000); // fake offset so server "now" is 1000000 + perf.now()
  global.performance._now = 0;
  // startAt = 1010000, estimatedServerNow = 1000000
  const rem = Timer.calculateRemainingMs(1010000);
  expect(rem).toBeCloseTo(10000, 0);
});

test('calculateRemainingMs: clamps to zero', () => {
  Timer._setOffset(1000000);
  global.performance._now = 0;
  // startAt already passed
  const rem = Timer.calculateRemainingMs(999000);
  expect(rem).toBe(0);
});

// ---- processTimeSyncResult ----

test('processTimeSyncResult: computes offset', () => {
  global.performance._now = 100; // at send time
  // Simulate receive time = 200ms later
  const recvNow = 200;

  // The message echoes client_send_ts=100
  // Server received at Unix ms 5000100, sent at Unix ms 5000101
  const msg = {
    client_send_ts: 100,
    server_recv_ts: 5000100,
    server_send_ts: 5000101,
  };

  // Manually set perf.now to recvNow so processTimeSyncResult reads it
  global.performance._now = recvNow;
  Timer.processTimeSyncResult(msg);

  const samples = Timer._getSamples();
  expect(samples.length).toBe(1);
  // rtt = 200 - 100 = 100ms
  expect(samples[0].rtt).toBeCloseTo(100, 0);
  // serverMid = (5000100 + 5000101)/2 = 5000100.5
  // clientMid = (100 + 200)/2 = 150
  // offset = 5000100.5 - 150 = 4999950.5
  expect(samples[0].offset).toBeCloseTo(4999950.5, 0);
});

test('processTimeSyncResult: keeps at most SYNC_SAMPLES samples', () => {
  global.performance._now = 0;
  for (let i = 0; i < 8; i++) {
    Timer.processTimeSyncResult({
      client_send_ts: 0,
      server_recv_ts: 1000000,
      server_send_ts: 1000001,
    });
  }
  expect(Timer._getSamples().length).toBe(5);
});

// ---- currentRTT ----

test('currentRTT: null when no samples', () => {
  expect(Timer.currentRTT()).toBeNull();
});

test('currentRTT: rounds to integer', () => {
  global.performance._now = 200;
  Timer.processTimeSyncResult({
    client_send_ts: 100,
    server_recv_ts: 5000100,
    server_send_ts: 5000101,
  });
  // rtt = 200 - 100 = 100
  expect(Timer.currentRTT()).toBe(100);
});

// ---- getCircumference ----

test('getCircumference: matches SVG r=120', () => {
  const c = Timer.getCircumference();
  expect(c).toBeCloseTo(2 * Math.PI * 120, 2);
});

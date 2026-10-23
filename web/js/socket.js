'use strict';

var Socket = (function () {
  var RECONNECT_BASE   = 500;
  var RECONNECT_MAX    = 30000;
  var RECONNECT_JITTER = 500;

  var ws           = null;
  var reconnecting = false;
  var retryCount   = 0;
  var retryTimer   = null;
  var currentRoom  = null;
  var handlers     = {};
  var connectCb    = null;
  var disconnectCb = null;

  function connect(roomID) {
    currentRoom = roomID;
    openWS();
  }

  function disconnect() {
    currentRoom = null;
    clearTimeout(retryTimer);
    if (ws) { ws.onclose = null; ws.close(); ws = null; }
    reconnecting = false;
    retryCount   = 0;
  }

  function openWS() {
    var proto = (location.protocol === 'https:') ? 'wss' : 'ws';
    var url   = proto + '://' + location.host + '/ws?room=' + currentRoom;
    ws = new WebSocket(url);

    ws.addEventListener('open', function () {
      reconnecting = false;
      retryCount   = 0;
      connectCb && connectCb();
    });

    ws.addEventListener('message', function (ev) {
      var msg;
      try { msg = JSON.parse(ev.data); } catch { return; }
      var h = handlers[msg.type];
      if (h) { h(msg); }
    });

    ws.addEventListener('close', function () {
      ws = null;
      if (!currentRoom) { return; }
      disconnectCb && disconnectCb();
      scheduleReconnect();
    });

    ws.addEventListener('error', function () { /* close follows */ });
  }

  function scheduleReconnect() {
    reconnecting = true;
    var delay = Math.min(RECONNECT_BASE * Math.pow(2, retryCount), RECONNECT_MAX)
              + Math.random() * RECONNECT_JITTER;
    retryCount++;
    retryTimer = setTimeout(openWS, delay);
  }

  function send(msg) {
    if (!ws || ws.readyState !== WebSocket.OPEN) { return false; }
    ws.send(JSON.stringify(msg));
    return true;
  }

  function on(type, fn)   { handlers[type] = fn; }
  function off(type)      { delete handlers[type]; }
  function onConnected(fn)    { connectCb    = fn; }
  function onDisconnected(fn) { disconnectCb = fn; }
  function isConnected()      { return ws !== null && ws.readyState === WebSocket.OPEN; }
  function isReconnecting()   { return reconnecting; }

  return {
    connect:        connect,
    disconnect:     disconnect,
    send:           send,
    on:             on,
    off:            off,
    onConnected:    onConnected,
    onDisconnected: onDisconnected,
    isConnected:    isConnected,
    isReconnecting: isReconnecting,
  };
})();

if (typeof module !== 'undefined' && module.exports) {
  module.exports = Socket;
}

function createSocketHandler(wsURL, callbacks) {
  var conn = null;
  var MIN_JSON_LINE_LENGTH = 10;

  function safeCallback(name, arg) {
    if (!callbacks || typeof callbacks[name] !== "function") return;
    callbacks[name](arg);
  }

  function parseJSON(line) {
    if (!line || line.length < MIN_JSON_LINE_LENGTH) return null;
    try {
      return JSON.parse(line);
    } catch (e) {
      return null;
    }
  }

  function dispatchRaw(raw) {
    var parsed = parseJSON(raw);
    if (!parsed) return;

    var normalized = normalizeEvent(parsed);
    if (!normalized) return;

    safeCallback("onMessage", normalized);
  }

  function handlePayload(payload) {
    if (!payload) return;

    if (payload.indexOf("\n") === -1) {
      dispatchRaw(payload);
      return;
    }

    var lines = payload.split("\n");
    for (var i = 0; i < lines.length; i++) {
      dispatchRaw(lines[i]);
    }
  }

  if (!window.WebSocket) {
    return {
      send: function () {},
      isOpen: function () {
        return false;
      },
    };
  }

  conn = new WebSocket(wsURL);

  conn.onopen = function () {
    safeCallback("onOpen");
  };

  conn.onclose = function () {
    safeCallback("onClose");
  };

  conn.onmessage = function (evt) {
    var payload = safeText(evt.data);
    handlePayload(payload);
  };

  return {
    send: function (data) {
      if (conn && conn.readyState === WebSocket.OPEN) {
        conn.send(data);
      }
    },
    isOpen: function () {
      return !!(conn && conn.readyState === WebSocket.OPEN);
    },
  };
}

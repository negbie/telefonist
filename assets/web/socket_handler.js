function createSocketHandler(wsURL, callbacks) {
  var conn = null;
  var MIN_JSON_LINE_LENGTH = 10;

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
    if (callbacks.onOpen) callbacks.onOpen();
  };

  conn.onclose = function () {
    if (callbacks.onClose) callbacks.onClose();
  };

  conn.onmessage = function (evt) {
    var payload = safeText(evt.data);

    // Fast path: most frames are a single JSON object.
    if (payload.indexOf("\n") === -1) {
      if (payload.length < MIN_JSON_LINE_LENGTH) return;

      var parsedSingle;
      try {
        parsedSingle = JSON.parse(payload);
      } catch (e) {
        return;
      }

      var singleEvent = normalizeEvent(parsedSingle);
      if (!singleEvent) return;

      if (callbacks.onMessage) {
        callbacks.onMessage(singleEvent);
      }
      return;
    }

    // Fallback: handle newline-delimited JSON payloads.
    var messages = payload.split("\n");
    for (var i = 0; i < messages.length; i++) {
      if (messages[i].length < MIN_JSON_LINE_LENGTH) continue;

      var parsed;
      try {
        parsed = JSON.parse(messages[i]);
      } catch (e) {
        continue;
      }

      var j = normalizeEvent(parsed);
      if (!j) continue;

      if (callbacks.onMessage) {
        callbacks.onMessage(j);
      }
    }
  };

  return {
    send: function (data) {
      if (conn && conn.readyState === WebSocket.OPEN) {
        conn.send(data);
      }
    },
    isOpen: function () {
      return conn && conn.readyState === WebSocket.OPEN;
    },
  };
}

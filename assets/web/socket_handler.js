import { safeText } from "./utils.js";
import { normalizeEvent } from "./flow.js";

export function createSocketHandler(wsURL, callbacks) {
  var conn = null;
  var MIN_JSON_LINE_LENGTH = 10;
  var MAX_LINES_PER_FRAME = 250;
  var payloadQueue = [];
  var activePayload = null;
  var activeIndex = 0;
  var processingPayloads = false;
  var queuedBytes = 0, lastSequence = 0, epoch = "", retry = 0, timer = null, disposed = false;

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
    if (parsed._epoch && parsed._epoch !== epoch) {
      if (epoch) safeCallback("onGap", "Server restarted; showing available history.");
      epoch = parsed._epoch; lastSequence = 0;
    }
    if (parsed._seq) {
      if (parsed._seq <= lastSequence) return;
      if (lastSequence && parsed._seq > lastSequence + 1)
        safeCallback("onGap", "Live history has a gap. Open the stored run for full evidence.");
      lastSequence = parsed._seq;
    }

    var normalized = normalizeEvent(parsed);
    if (!normalized) return;

    safeCallback("onMessage", normalized);
  }

  function schedulePayloadProcessing() {
    if (processingPayloads) return;
    processingPayloads = true;
    if (typeof requestAnimationFrame === "function") {
      requestAnimationFrame(processPayloadQueue);
    } else {
      setTimeout(processPayloadQueue, 0);
    }
  }

  function nextQueuedLine() {
    for (;;) {
      if (activePayload === null) {
        if (payloadQueue.length === 0) return null;
        activePayload = payloadQueue.shift();
        queuedBytes -= activePayload.length;
        activeIndex = 0;
      }

      var newlineIndex = activePayload.indexOf("\n", activeIndex);
      if (newlineIndex === -1) {
        var lastLine = activePayload.slice(activeIndex);
        activePayload = null;
        activeIndex = 0;
        return lastLine;
      }

      var line = activePayload.slice(activeIndex, newlineIndex);
      activeIndex = newlineIndex + 1;
      return line;
    }
  }

  function processPayloadQueue() {
    var processed = 0;
    while (processed < MAX_LINES_PER_FRAME) {
      var line = nextQueuedLine();
      if (line === null) {
        processingPayloads = false;
        return;
      }
      dispatchRaw(line);
      processed++;
    }

    if (typeof requestAnimationFrame === "function") {
      requestAnimationFrame(processPayloadQueue);
    } else {
      setTimeout(processPayloadQueue, 0);
    }
  }

  function handlePayload(payload) {
    if (!payload) return;
    if (payload.length + queuedBytes > 8 * 1024 * 1024) {
      safeCallback("onGap", "Live display overloaded. Open the stored run for full evidence.");
      payloadQueue = []; queuedBytes = 0; activePayload = null;
      conn.close(); return;
    }

    if (!processingPayloads && payloadQueue.length === 0 && payload.indexOf("\n") === -1) {
      dispatchRaw(payload);
      return;
    }

    payloadQueue.push(payload);
    queuedBytes += payload.length;
    schedulePayloadProcessing();
  }

  if (!window.WebSocket) {
    return {
      send: function () {},
      isOpen: function () {
        return false;
      },
    };
  }

  function connect() {
  if (disposed) return;
  conn = new WebSocket(wsURL);

  conn.onopen = function () {
    retry = 0;
    safeCallback("onOpen");
  };

  conn.onclose = function () {
    safeCallback("onClose");
    if (!disposed) {
      var delay = Math.min(3000, 500 * Math.min(retry++, 6)) + Math.random() * 250;
      timer = setTimeout(connect, delay);
    }
  };

  conn.onmessage = function (evt) {
    var payload = safeText(evt.data);
    handlePayload(payload);
  };
  }

  function handleQuickReconnect() {
    if (!disposed && (!conn || conn.readyState === 3)) {
      clearTimeout(timer);
      retry = 0;
      connect();
    }
  }

  if (typeof window !== "undefined" && typeof window.addEventListener === "function") {
    window.addEventListener("focus", handleQuickReconnect);
    window.addEventListener("online", handleQuickReconnect);
  }
  if (typeof document !== "undefined" && typeof document.addEventListener === "function") {
    document.addEventListener("visibilitychange", function () {
      if (document.visibilityState === "visible") {
        handleQuickReconnect();
      }
    });
  }

  connect();

  return {
    close: function () {
      disposed = true;
      clearTimeout(timer);
      if (typeof window !== "undefined" && typeof window.removeEventListener === "function") {
        window.removeEventListener("focus", handleQuickReconnect);
        window.removeEventListener("online", handleQuickReconnect);
      }
      if (conn) {
        conn.close();
      }
    },
    send: function (data) {
      if (conn && conn.readyState === (window.WebSocket ? window.WebSocket.OPEN : 1)) {
        conn.send(data);
      }
    },
    isOpen: function () {
      return !!(conn && conn.readyState === (window.WebSocket ? window.WebSocket.OPEN : 1));
    },
  };
}


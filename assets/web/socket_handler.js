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

    if (!processingPayloads && payloadQueue.length === 0 && payload.indexOf("\n") === -1) {
      dispatchRaw(payload);
      return;
    }

    payloadQueue.push(payload);
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


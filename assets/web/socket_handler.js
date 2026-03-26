function createSocketHandler(wsURL, callbacks) {
    var conn = null;

    if (!window.WebSocket) {
        return {
            send: function () { },
            isOpen: function () { return false; }
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
        var messages = safeText(evt.data).split("\n");
        for (var i = 0; i < messages.length; i++) {
            if (messages[i].length < 10) continue;

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
            if (conn && conn.readyState === 1) {
                conn.send(data);
            }
        },
        isOpen: function () {
            return conn && conn.readyState === 1;
        }
    };
}

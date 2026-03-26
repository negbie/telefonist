/* event_bus.js — Tiny pub/sub for decoupling frontend modules. */
window.EventBus = (function () {
    var listeners = {};

    return {
        on: function (event, fn) {
            if (!listeners[event]) listeners[event] = [];
            listeners[event].push(fn);
        },
        emit: function (event) {
            var args = Array.prototype.slice.call(arguments, 1);
            var fns = listeners[event];
            if (!fns) return;
            for (var i = 0; i < fns.length; i++) {
                fns[i].apply(null, args);
            }
        }
    };
})();

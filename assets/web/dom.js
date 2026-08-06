import { safeText } from "./utils.js";

export function isNearBottom(el, thresholdPx) {
  if (thresholdPx === undefined) thresholdPx = 25;
  return el.scrollHeight - el.scrollTop - el.clientHeight < thresholdPx;
}

export function scrollToBottom(el, behavior) {
  if (behavior === "smooth") {
    el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
  } else {
    el.scrollTop = el.scrollHeight;
  }
}

export function trimChildrenToMax(parent, max) {
  if (!parent || !max || max <= 0) return;
  var children = parent.children;
  if (!children || children.length <= max) return;

  var overflow = children.length - max;
  for (var i = 0; i < overflow; i++) {
    var child = parent.firstChild;
    if (!child) break;
    parent.removeChild(child);
  }
}

export function ensureElement(parent, tag, className) {
  var el = document.createElement(tag);
  if (className) el.className = className;
  parent.appendChild(el);
  return el;
}

var appendQueues = new WeakMap();

function scheduleFrame(fn) {
  if (typeof requestAnimationFrame === "function") {
    requestAnimationFrame(fn);
  } else {
    setTimeout(fn, 0);
  }
}

export function clearAppendQueue(container) {
  if (container) appendQueues.delete(container);
}

function flushAppendQueue(container) {
  var queue = appendQueues.get(container);
  if (!queue) return;
  appendQueues.delete(container);

  var fragment = document.createDocumentFragment();
  var maxItems = 0;
  var doScroll = false;

  for (var i = 0; i < queue.length; i++) {
    fragment.appendChild(queue[i].el);
    maxItems = queue[i].maxItems;
    doScroll = doScroll || queue[i].doScroll;
  }

  container.appendChild(fragment);
  trimChildrenToMax(container, maxItems);

  if (doScroll) {
    scrollToBottom(container);
  }
}

function appendImmediately(container, el, maxItems, doScroll) {
  container.appendChild(el);
  trimChildrenToMax(container, maxItems);

  if (doScroll) {
    scrollToBottom(container);
  }
}

export function appendAndMaintain(container, el, opts) {
  if (!container || !el) return;
  var options = opts || {};
  var maxItems = options.maxItems || 0;
  var doScroll = !!options.autoscroll && isNearBottom(container);

  if (
    el.classList &&
    (el.classList.contains("run-separator") ||
      el.classList.contains("download-container"))
  ) {
    flushAppendQueue(container);
    appendImmediately(container, el, maxItems, doScroll);
    return;
  }

  var queue = appendQueues.get(container);
  if (!queue) {
    queue = [];
    appendQueues.set(container, queue);
    scheduleFrame(function () {
      flushAppendQueue(container);
    });
  }
  queue.push({ el: el, maxItems: maxItems, doScroll: doScroll });
}

export function tagToken(el, token) {
  if (!el) return;
  var t = safeText(token || "");
  if (!t) return;
  el.setAttribute("data-token", t);
}

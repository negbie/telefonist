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

export function appendAndMaintain(container, el, opts) {
  if (!container || !el) return;
  var options = opts || {};
  var maxItems = options.maxItems || 0;
  var doScroll = !!options.autoscroll && isNearBottom(container);

  container.appendChild(el);
  trimChildrenToMax(container, maxItems);

  if (doScroll) {
    scrollToBottom(container);
  }
}

export function tagToken(el, token) {
  if (!el) return;
  var t = safeText(token || "");
  if (!t) return;
  el.setAttribute("data-token", t);
}

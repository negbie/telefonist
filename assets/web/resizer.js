export function initResizer(resizer, topRow, bottomRow) {
  if (!resizer || !topRow || !bottomRow) return;

  var minTopPx = 100;
  var minBottomPx = 100;
  var isResizing = false;
  var activePointerId = null;
  var latestClientY = 0;
  var ticking = false;
  var lastAppliedTopPx = -1;

  function clampTopPx(px, totalH) {
    var min = minTopPx;
    var max = Math.max(min, totalH - minBottomPx);
    if (px < min) return min;
    if (px > max) return max;
    return px;
  }

  function totalHeight() {
    return window.innerHeight || 0;
  }

  var cachedTotalH = 0;
  var topRowEl = topRow;

  function scheduleApply() {
    if (ticking) return;
    ticking = true;
    requestAnimationFrame(function () {
      var px = clampTopPx(latestClientY, cachedTotalH);
      if (px !== lastAppliedTopPx) {
        if (topRowEl) topRowEl.style.height = px + "px";
        lastAppliedTopPx = px;
      }
      ticking = false;
    });
  }

  function startResize(e) {
    isResizing = true;
    activePointerId = e.pointerId;
    latestClientY = e.clientY;
    cachedTotalH = totalHeight();
    document.body.classList.add("resizing");
    document.body.style.cursor = "ns-resize";
    if (resizer.setPointerCapture) {
      resizer.setPointerCapture(activePointerId);
    }
    scheduleApply();
    e.preventDefault();
  }

  function stopResize() {
    if (!isResizing) return;
    isResizing = false;
    if (resizer.releasePointerCapture && activePointerId != null) {
      try {
        resizer.releasePointerCapture(activePointerId);
      } catch (_) {}
    }
    activePointerId = null;
    document.body.classList.remove("resizing");
    document.body.style.cursor = "";
  }

  resizer.addEventListener("pointerdown", function (e) {
    if (e.button !== 0) return;
    startResize(e);
  });

  resizer.addEventListener("pointermove", function (e) {
    if (!isResizing) return;
    if (activePointerId !== null && e.pointerId !== activePointerId) return;
    latestClientY = e.clientY;
    scheduleApply();
  });

  resizer.addEventListener("pointerup", function (e) {
    if (activePointerId !== null && e.pointerId !== activePointerId) return;
    stopResize();
  });

  resizer.addEventListener("pointercancel", function (e) {
    if (activePointerId !== null && e.pointerId !== activePointerId) return;
    stopResize();
  });

  window.addEventListener("blur", stopResize);

  var initialH = totalHeight();
  var topPx = clampTopPx(Math.round(initialH / 2), initialH);
  if (topRowEl) topRowEl.style.height = topPx + "px";
  lastAppliedTopPx = topPx;
}

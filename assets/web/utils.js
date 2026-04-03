export function nowLocalString() {
  return new Date().toLocaleString();
}

export function safeText(value) {
  return value == null ? "" : String(value);
}

export function stripANSI(s) {
  return safeText(s).replace(
    /[\u001b\u009b][[()#;?]*(?:[0-9]{1,4}(?:;[0-9]{0,4})*)?[0-9A-ORZcf-nqry=><]/g,
    "",
  );
}

export function base64EncodeUTF8(s) {
  return btoa(
    encodeURIComponent(s).replace(/%([0-9A-F]{2})/g, function (_, p1) {
      return String.fromCharCode(parseInt(p1, 16));
    }),
  );
}

export function base64DecodeUTF8(b64) {
  var binary = atob(safeText(b64));
  var bytes = new Uint8Array(binary.length);
  for (var i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return new TextDecoder().decode(bytes);
}

export function isNearBottom(el, thresholdPx) {
  if (thresholdPx === undefined) thresholdPx = 6;
  return el.scrollHeight - el.scrollTop - el.clientHeight < thresholdPx;
}

export function scrollToBottom(el) {
  el.scrollTop = el.scrollHeight - el.clientHeight;
}

export function trimChildrenToMax(parent, max) {
  if (!parent || !max || max <= 0) return;
  var children = parent.children;
  if (!children) return;
  var overflow = children.length - max;
  for (var i = 0; i < overflow; i++) {
    parent.removeChild(parent.firstChild);
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
  var autoscroll = !!options.autoscroll;

  container.appendChild(el);
  trimChildrenToMax(container, maxItems);
  if (autoscroll) {
    var targetScrollTop = container.scrollHeight - container.clientHeight;
    if (container.scrollTop !== targetScrollTop) {
      container.scrollTop = targetScrollTop;
    }
  }
}

export function tagToken(el, token) {
  if (!el) return;
  var t = safeText(token || "");
  if (!t) return;
  el.setAttribute("data-token", t);
}

export function sanitizeTestfileName(raw) {
  var s = safeText(raw || "").replace(/\s+/g, "");
  s = s.replace(/[^A-Za-z0-9._-]/g, "");
  return s.slice(0, 64);
}

export function escapeHTML(str) {
  return safeText(str)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");
}

export function wireSearchFilter(inputEl, containerEl, selector) {
  if (!inputEl || !containerEl) return;

  var lastFilter = null;

  function applyFilter() {
    var filter = safeText(inputEl.value).toLowerCase();
    if (filter === lastFilter) return;
    lastFilter = filter;

    var nodes = containerEl.querySelectorAll(selector);
    for (var i = 0; i < nodes.length; i++) {
      var node = nodes[i];
      var text = safeText(node.textContent).toLowerCase();
      node.style.display = text.includes(filter) ? "" : "none";
    }
  }

  inputEl.addEventListener("input", applyFilter);
  applyFilter();
}

export function syntaxHighlight(inputEl, highlightsEl) {
  if (!inputEl || !highlightsEl) return;

  var text = safeText(inputEl.value);
  var lines = text.split("\n");
  var html = "";

  var palette = [
    "hsl(210, 75%, 40%)", // Blue
    "hsl(120, 65%, 35%)", // Green
    "hsl(0, 75%, 40%)", // Red
    "hsl(25, 85%, 40%)", // Orange
    "hsl(265, 70%, 45%)", // Indigo
    "hsl(50, 90%, 35%)", // Yellow-ish
    "hsl(300, 65%, 40%)", // Violet
  ];

  var vars = {};
  var colorIdx = 0;
  for (var i = 0; i < lines.length; i++) {
    var trimmed = lines[i].trim();
    if (trimmed.startsWith("_define ")) {
      var parts = trimmed.split(/\s+/);
      if (parts.length >= 2) {
        var name = parts[1];
        if (!vars[name]) {
          vars[name] = palette[colorIdx % palette.length];
          colorIdx++;
        }
      }
    }
  }

  function escapeRegExp(value) {
    return value.replace(/[-\/\\^$*+?.()|[\]{}]/g, "\\$&");
  }

  function wrapVarToken(token) {
    return (
      '<span class="hl-var" style="color: ' +
      vars[token] +
      '; font-weight: bold;">' +
      escapeHTML(token) +
      "</span>"
    );
  }

  function highlightVars(rawText) {
    var escaped = escapeHTML(rawText);
    var varNames = Object.keys(vars);
    if (varNames.length === 0) return escaped;

    varNames.sort(function (a, b) {
      return b.length - a.length;
    });

    var pattern = new RegExp(
      "\\b(" + varNames.map(escapeRegExp).join("|") + ")\\b",
      "g",
    );

    return escaped.replace(pattern, function (matched) {
      return wrapVarToken(matched);
    });
  }

  for (var j = 0; j < lines.length; j++) {
    var line = lines[j];
    var trimmed = line.trim();

    if (trimmed.startsWith("#")) {
      html += '<span class="hl-comment">' + escapeHTML(line) + "</span>";
    } else if (trimmed.startsWith("_")) {
      var parts = line.match(/^(\s*)(_[a-zA-Z0-9]+)(\s+)(.*)$/);
      if (parts) {
        if (parts[1]) html += '<span class="hl-space">' + parts[1] + "</span>";
        html +=
          '<span class="hl-directive">' + escapeHTML(parts[2]) + "</span>";
        html += '<span class="hl-space">' + parts[3] + "</span>";

        if (parts[2] === "_define") {
          var valParts = parts[4].match(/^(\S+)(\s+)(.*)$/);
          if (valParts) {
            html += wrapVarToken(valParts[1]);
            html += '<span class="hl-space">' + valParts[2] + "</span>";
            html +=
              '<span class="hl-value">' + escapeHTML(valParts[3]) + "</span>";
          } else {
            html += wrapVarToken(parts[4]);
          }
        } else {
          html +=
            '<span class="hl-value">' + highlightVars(parts[4]) + "</span>";
        }
      } else {
        html += '<span class="hl-directive">' + escapeHTML(line) + "</span>";
      }
    } else {
      var rem = line;
      var nameMatch = rem.match(/^(\s*)([a-zA-Z0-9_.-]+:)(.*)$/);
      if (nameMatch) {
        if (nameMatch[1])
          html += '<span class="hl-space">' + nameMatch[1] + "</span>";
        html +=
          '<span class="hl-name">' + highlightVars(nameMatch[2]) + "</span>";
        rem = nameMatch[3] || "";
      }

      if (rem) {
        var pSplit = rem.split("|");
        for (var p = 0; p < pSplit.length; p++) {
          if (p > 0) html += '<span class="hl-pipe">|</span>';
          var tkMatch = pSplit[p].match(/^(\s*)(.*?)(\s*)$/);
          if (tkMatch) {
            if (tkMatch[1])
              html += '<span class="hl-space">' + tkMatch[1] + "</span>";
            var tk = tkMatch[2];
            if (tk) {
              html += '<span class="hl-cmd">' + highlightVars(tk) + "</span>";
            }
            if (tkMatch[3])
              html += '<span class="hl-space">' + tkMatch[3] + "</span>";
          }
        }
      }
    }
    if (j < lines.length - 1) html += "\n";
  }

  if (text.endsWith("\n")) html += " ";
  highlightsEl.innerHTML = html;
}

export function wsURL() {
  var scheme = window.location.protocol === "https:" ? "wss://" : "ws://";
  return scheme + window.location.host + "/ws";
}

export function setBodyFlowView() {
  document.body.setAttribute("data-view", "flow");
}

export function setBodyToggleAttribute(checkboxEl, attrName) {
  if (!checkboxEl) return;
  if (checkboxEl.checked) {
    document.body.setAttribute(attrName, "1");
  } else {
    document.body.removeAttribute(attrName);
  }
}

export function applyOnlyTestsFilterFromCheckbox(onlyTestsEl) {
  setBodyToggleAttribute(onlyTestsEl, "data-only-tests");
}

export function applyOnlyResultsFilterFromCheckbox(onlyResultsEl) {
  setBodyToggleAttribute(onlyResultsEl, "data-only-results");
}

export function initResizer(resizer, topRow, bottomRow) {
  if (!resizer || !topRow || !bottomRow) return;

  var minTopPx = 100;
  var minBottomPx = 100;
  var isResizing = false;
  var activePointerId = null;
  var latestClientY = 0;
  var ticking = false;
  var topPx = 0;
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
  var bottomRowEl = bottomRow;

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
  topPx = clampTopPx(Math.round(initialH / 2), initialH);
  if (topRowEl) topRowEl.style.height = topPx + "px";
  lastAppliedTopPx = topPx;
}

export function computeLCSDiff(aItems, bItems) {
  var m = aItems.length,
    n = bItems.length;
  var dp = Array.from({ length: m + 1 }, function () {
    return new Uint32Array(n + 1);
  });

  for (var i = 1; i <= m; i++) {
    for (var j = 1; j <= n; j++) {
      dp[i][j] =
        aItems[i - 1].compare === bItems[j - 1].compare
          ? dp[i - 1][j - 1] + 1
          : Math.max(dp[i - 1][j], dp[i][j - 1]);
    }
  }

  var result = [];
  var ci = m,
    cj = n;
  while (ci > 0 || cj > 0) {
    if (ci > 0 && cj > 0 && aItems[ci - 1].compare === bItems[cj - 1].compare) {
      result.unshift({ type: "common", text: aItems[ci - 1].display });
      ci--;
      cj--;
    } else if (cj > 0 && (ci === 0 || dp[ci][cj - 1] >= dp[ci - 1][cj])) {
      result.unshift({ type: "b", text: bItems[cj - 1].display });
      cj--;
    } else {
      result.unshift({ type: "a", text: aItems[ci - 1].display });
      ci--;
    }
  }
  return result;
}



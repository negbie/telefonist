function nowLocalString() { return new Date().toLocaleString(); }

function safeText(v) { return (v === null || v === undefined ? "" : String(v)); }

function stripANSI(s) {
    return safeText(s).replace(
        /[\u001b\u009b][[()#;?]*(?:[0-9]{1,4}(?:;[0-9]{0,4})*)?[0-9A-ORZcf-nqry=><]/g,
        "",
    );
}

function base64EncodeUTF8(s) {
    return btoa(
        encodeURIComponent(s).replace(/%([0-9A-F]{2})/g, function (_, p1) {
            return String.fromCharCode(parseInt(p1, 16));
        }),
    );
}

function base64DecodeUTF8(b64) {
    var bin = atob(String(b64 || ""));
    var bytes = new Uint8Array(bin.length);
    for (var i = 0; i < bin.length; i++) {
        bytes[i] = bin.charCodeAt(i);
    }
    return new TextDecoder().decode(bytes);
}

function isNearBottom(el, thresholdPx) {
    if (thresholdPx === undefined) thresholdPx = 6;
    return el.scrollHeight - el.scrollTop - el.clientHeight < thresholdPx;
}

function scrollToBottom(el) {
    el.scrollTop = el.scrollHeight - el.clientHeight;
}

function trimChildrenToMax(parent, max) {
    if (!parent || !max || max <= 0) return;
    while (parent.children && parent.children.length > max) {
        parent.removeChild(parent.firstChild);
    }
}

function ensureElement(parent, tag, className) {
    var el = document.createElement(tag);
    if (className) el.className = className;
    parent.appendChild(el);
    return el;
}

function tagToken(el, token) {
    if (!el) return;
    var t = safeText(token || "");
    if (!t) return;
    el.setAttribute("data-token", t);
}

function sanitizeTestfileName(raw) {
    var s = safeText(raw || "").replace(/\s+/g, "");
    s = s.replace(/[^A-Za-z0-9._-]/g, "");
    return s.slice(0, 64);
}

function escapeHTML(str) {
    return String(str || "")
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#039;");
}

function wireSearchFilter(inputEl, containerEl, selector) {
    if (!inputEl || !containerEl) return;
    var apply = function () {
        var filter = (inputEl.value || "").toLowerCase();
        var nodes = containerEl.querySelectorAll(selector);
        for (var i = 0; i < nodes.length; i++) {
            var node = nodes[i];
            node.style.display = node.textContent.toLowerCase().includes(filter) ? "" : "none";
        }
    };
    inputEl.addEventListener("input", apply);
    apply();
}

function syntaxHighlight(inputEl, highlightsEl) {
    if (!inputEl || !highlightsEl) return;
    var text = inputEl.value || "";
    var lines = text.split("\n");
    var html = "";

    var palette = [
        "hsl(210, 75%, 40%)", // Blue
        "hsl(120, 65%, 35%)", // Green
        "hsl(0, 75%, 40%)",   // Red
        "hsl(25, 85%, 40%)",  // Orange
        "hsl(265, 70%, 45%)", // Indigo
        "hsl(50, 90%, 35%)",  // Yellow-ish
        "hsl(300, 65%, 40%)"  // Violet
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

    var highlightVars = function (text) {
        var escaped = escapeHTML(text);
        var varNames = Object.keys(vars);
        if (varNames.length === 0) return escaped;

        varNames.sort(function (a, b) { return b.length - a.length; });

        var pattern = new RegExp("\\b(" + varNames.map(function (v) { return v.replace(/[-\/\\^$*+?.()|[\]{}]/g, "\\$&"); }).join("|") + ")\\b", "g");
        return escaped.replace(pattern, function (matched) {
            return '<span class="hl-var" style="color: ' + vars[matched] + '; font-weight: bold;">' + matched + '</span>';
        });
    };

    for (var j = 0; j < lines.length; j++) {
        var line = lines[j];
        var trimmed = line.trim();

        if (trimmed.startsWith("#")) {
            html += '<span class="hl-comment">' + escapeHTML(line) + '</span>';
        } else if (trimmed.startsWith("_")) {
            var parts = line.match(/^(\s*)(_[a-zA-Z0-9]+)(\s+)(.*)$/);
            if (parts) {
                if (parts[1]) html += '<span class="hl-space">' + parts[1] + '</span>';
                html += '<span class="hl-directive">' + escapeHTML(parts[2]) + '</span>';
                html += '<span class="hl-space">' + parts[3] + '</span>';

                if (parts[2] === "_define") {
                    var valParts = parts[4].match(/^(\S+)(\s+)(.*)$/);
                    if (valParts) {
                        html += '<span class="hl-var" style="color: ' + vars[valParts[1]] + '; font-weight: bold;">' + escapeHTML(valParts[1]) + '</span>';
                        html += '<span class="hl-space">' + valParts[2] + '</span>';
                        html += '<span class="hl-value">' + escapeHTML(valParts[3]) + '</span>';
                    } else {
                        html += '<span class="hl-var" style="color: ' + vars[parts[4]] + '; font-weight: bold;">' + escapeHTML(parts[4]) + '</span>';
                    }
                } else {
                    html += '<span class="hl-value">' + highlightVars(parts[4]) + '</span>';
                }
            } else {
                html += '<span class="hl-directive">' + escapeHTML(line) + '</span>';
            }
        } else {
            var rem = line;
            var nameMatch = rem.match(/^(\s*)([a-zA-Z0-9_.-]+:)(.*)$/);
            if (nameMatch) {
                if (nameMatch[1]) html += '<span class="hl-space">' + nameMatch[1] + '</span>';
                html += '<span class="hl-name">' + highlightVars(nameMatch[2]) + '</span>';
                rem = nameMatch[3] || "";
            }

            if (rem) {
                var pSplit = rem.split("|");
                for (var p = 0; p < pSplit.length; p++) {
                    if (p > 0) html += '<span class="hl-pipe">|</span>';
                    var tkMatch = pSplit[p].match(/^(\s*)(.*?)(\s*)$/);
                    if (tkMatch) {
                        if (tkMatch[1]) html += '<span class="hl-space">' + tkMatch[1] + '</span>';
                        var tk = tkMatch[2];
                        if (tk) {
                            html += '<span class="hl-cmd">' + highlightVars(tk) + '</span>';
                        }
                        if (tkMatch[3]) html += '<span class="hl-space">' + tkMatch[3] + '</span>';
                    }
                }
            }
        }
        if (j < lines.length - 1) html += "\n";
    }

    if (text.endsWith("\n")) html += " ";
    highlightsEl.innerHTML = html;
}

function wsURL() {
    var scheme = window.location.protocol === "https:" ? "wss://" : "ws://";
    return scheme + window.location.host + "/ws";
}

function setBodyFlowView() {
    document.body.setAttribute("data-view", "flow");
}

function applyOnlyTestsFilterFromCheckbox(onlyTestsEl) {
    if (!onlyTestsEl) return;
    if (onlyTestsEl.checked) {
        document.body.setAttribute("data-only-tests", "1");
    } else {
        document.body.removeAttribute("data-only-tests");
    }
}

function applyOnlyResultsFilterFromCheckbox(onlyResultsEl) {
    if (!onlyResultsEl) return;
    if (onlyResultsEl.checked) {
        document.body.setAttribute("data-only-results", "1");
    } else {
        document.body.removeAttribute("data-only-results");
    }
}

function initResizer(resizer, topRow, bottomRow) {
    if (!resizer || !topRow || !bottomRow) return;
    var isResizing = false;

    resizer.addEventListener("mousedown", function (e) {
        isResizing = true;
        document.body.style.cursor = "ns-resize";
        e.preventDefault();
    });
    let ticking = false;

    document.addEventListener("mousemove", function (e) {
        if (!isResizing || ticking) return;

        ticking = true;
        requestAnimationFrame(() => {
            var totalH = document.body.clientHeight;
            var topH = e.clientY;
            if (topH < 100) topH = 100;
            if (totalH - topH < 100) topH = totalH - 100;
            var topPct = (topH / totalH) * 100;
            topRow.style.flex = "1 1 " + topPct + "%";
            bottomRow.style.flex = "1 1 " + (100 - topPct) + "%";
            ticking = false;
        });
    });
    document.addEventListener("mouseup", function () {
        if (isResizing) {
            isResizing = false;
            document.body.style.cursor = "";
        }
    });
}

function computeLCSDiff(aItems, bItems) {
    var m = aItems.length, n = bItems.length;
    var dp = Array.from({ length: m + 1 }, function () { return new Uint32Array(n + 1); });

    for (var i = 1; i <= m; i++) {
        for (var j = 1; j <= n; j++) {
            dp[i][j] = (aItems[i - 1].compare === bItems[j - 1].compare)
                ? dp[i - 1][j - 1] + 1
                : Math.max(dp[i - 1][j], dp[i][j - 1]);
        }
    }

    var result = [];
    var ci = m, cj = n;
    while (ci > 0 || cj > 0) {
        if (ci > 0 && cj > 0 && aItems[ci - 1].compare === bItems[cj - 1].compare) {
            result.unshift({ type: "common", text: aItems[ci - 1].display });
            ci--; cj--;
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

// Expose globals for other scripts (optional but helpful for clarity)
window.nowLocalString = nowLocalString;
window.safeText = safeText;
window.stripANSI = stripANSI;
window.base64EncodeUTF8 = base64EncodeUTF8;
window.base64DecodeUTF8 = base64DecodeUTF8;
window.isNearBottom = isNearBottom;
window.scrollToBottom = scrollToBottom;
window.trimChildrenToMax = trimChildrenToMax;
window.ensureElement = ensureElement;
window.tagToken = tagToken;
window.sanitizeTestfileName = sanitizeTestfileName;
window.escapeHTML = escapeHTML;
window.wireSearchFilter = wireSearchFilter;
window.syntaxHighlight = syntaxHighlight;
window.wsURL = wsURL;
window.setBodyFlowView = setBodyFlowView;
window.applyOnlyTestsFilterFromCheckbox = applyOnlyTestsFilterFromCheckbox;
window.applyOnlyResultsFilterFromCheckbox = applyOnlyResultsFilterFromCheckbox;
window.initResizer = initResizer;
window.computeLCSDiff = computeLCSDiff;

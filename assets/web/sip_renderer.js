import { safeText, escapeHTML } from "./utils.js";
import { trimChildrenToMax, appendAndMaintain } from "./dom.js";

export function truncateText(text, maxLen) {
  var s = safeText(text);
  return s.length > maxLen ? s.substring(0, maxLen) + "..." : s;
}

export function getSipMessageInfo(param) {
  var text = safeText(param);
  var lines = text.split("\n");
  var envelopeOffset = 0;
  var direction = "TX";
  var timeStr = "";
  var src = "Unknown";
  var dst = "Unknown";

  if (lines.length >= 2) {
    var parts = safeText(lines[0].replace("#", "")).split("|");
    timeStr = parts[0].trim();
    if (parts.length > 1) {
      direction = parts[1].trim().toUpperCase();
    }
    var arrowMatch = safeText(lines[1]).match(
      /(\w+)\s+([0-9\.\:a-fA-F\[\]]+)\s+->\s+([0-9\.\:a-fA-F\[\]]+)/,
    );
    if (arrowMatch) {
      src = arrowMatch[2];
      dst = arrowMatch[3];
      envelopeOffset = 2;
    } else if (/^\w+\s+/.test(safeText(lines[1]))) {
      envelopeOffset = 2;
    }
  }

  var message = envelopeOffset ? lines.slice(envelopeOffset).join("\n") : text;
  var firstLine = (message.split(/\r?\n/).find((line) => line.trim()) || "").trim();
  var requestMatch = firstLine.match(/^([A-Z][A-Z0-9_-]*)\s+\S+\s+SIP\/2\.0\b/);
  var responseMatch = firstLine.match(/^SIP\/2\.0\s+(\d{3})(?:\s+(.*))?/);
  var cseqMatch = message.match(/^CSeq:\s*(\d+)\s+([A-Z][A-Z0-9_-]*)\b/im);
  var callIdMatch = message.match(/^Call-ID:\s*(\S+)/im);

  var cseqNumber = cseqMatch ? parseInt(cseqMatch[1], 10) : null;
  var cseqMethod = cseqMatch ? cseqMatch[2].toUpperCase() : "";
  var callId = callIdMatch ? callIdMatch[1].trim() : "";

  var method = requestMatch ? requestMatch[1].toUpperCase() : (cseqMethod || "");
  var statusCode = responseMatch ? parseInt(responseMatch[1], 10) : 0;
  var statusText = responseMatch ? safeText(responseMatch[2]).trim() : "";

  var topText = method || firstLine;
  var bottomText = responseMatch ? statusCode + (statusText ? " " + statusText : "") : "";
  var label = topText + (bottomText ? " / " + bottomText : "");

  return {
    bottomText,
    callId,
    cseqMethod,
    cseqNumber,
    direction,
    dst,
    firstLine,
    isResponse: Boolean(responseMatch),
    label: label || "SIP",
    message,
    method,
    src,
    statusCode,
    statusText,
    timeStr,
    topText,
  };
}

export function getSipDiffKey(param) {
  var info = getSipMessageInfo(param);
  return [info.topText, info.bottomText].filter(Boolean).join(" ") || safeText(param);
}

/**
 * Analyzes an array of SIP events to track message retransmissions
 * and detect unacknowledged INVITE final responses (Missing ACKs).
 */
export function analyzeSipEvents(events) {
  if (!Array.isArray(events)) return [];

  var parsed = events.map(function (e) {
    return getSipMessageInfo(e ? e.param || "" : "");
  });

  // Track all ACKs by `${callId}|${cseqNumber}`
  var acks = new Set();
  parsed.forEach(function (info) {
    if (info.cseqMethod === "ACK" || info.method === "ACK") {
      if (info.cseqNumber !== null) {
        acks.add((info.callId || "") + "|" + info.cseqNumber);
      }
    }
  });

  // Track retransmissions per message signature
  var sigCounts = new Map();
  return parsed.map(function (info) {
    var sig = [
      info.direction,
      info.callId,
      info.cseqNumber,
      info.cseqMethod,
      info.statusCode || info.method,
    ].join("|");

    var count = (sigCounts.get(sig) || 0) + 1;
    sigCounts.set(sig, count);
    var retransmitCount = count - 1;

    // Missing ACK: INVITE final response (>= 200) without matching ACK
    var missingAck = false;
    if (info.cseqMethod === "INVITE" && info.isResponse && info.statusCode >= 200) {
      if (info.cseqNumber !== null) {
        var ackKey = (info.callId || "") + "|" + info.cseqNumber;
        if (!acks.has(ackKey)) {
          missingAck = true;
        }
      }
    }

    return {
      callId: info.callId,
      cseqMethod: info.cseqMethod,
      cseqNumber: info.cseqNumber,
      missingAck: missingAck,
      retransmitCount: retransmitCount,
    };
  });
}

function ensureMissingAckBadge(rowEl, cseqNum) {
  var botEl = rowEl.querySelector(".sip-method-bottom");
  if (!botEl) {
    var lineEl = rowEl.querySelector(".sip-arrow-line");
    if (!lineEl) return;
    botEl = document.createElement("div");
    botEl.className = "sip-method-bottom";
    var headEl = lineEl.querySelector(".sip-arrow-head");
    if (headEl) lineEl.insertBefore(botEl, headEl);
    else lineEl.appendChild(botEl);
  }
  if (!botEl.querySelector(".sip-badge-missing-ack")) {
    var badge = document.createElement("span");
    badge.className = "sip-badge sip-badge-missing-ack";
    badge.title = "No ACK received from peer for CSeq " + (cseqNum != null ? cseqNum : "");
    badge.textContent = "⚠️ Missing ACK";
    botEl.appendChild(badge);
  }
}

function removeMissingAckBadge(rowEl) {
  var badge = rowEl.querySelector(".sip-badge-missing-ack");
  if (badge) badge.remove();
}

export function generateSipLadderHTML(j, msgCount, meta) {
  var type = (j.type || j.token || "").toUpperCase();
  if (type !== "SIP") return "";
  var param = j.param || "";
  var sipInfo = getSipMessageInfo(param);

  if (!sipInfo.firstLine && !sipInfo.method) {
    return `<div class="sip-ladder-row" style="padding: 10px;">${escapeHTML(param)}</div>`;
  }

  meta = meta || {};
  var cseqNum = meta.cseqNumber !== undefined ? meta.cseqNumber : sipInfo.cseqNumber;
  var retransmitCount = meta.retransmitCount || 0;
  var missingAck = Boolean(meta.missingAck);

  var methodColor = "#000";
  if (sipInfo.isResponse) {
    if (sipInfo.statusCode >= 200 && sipInfo.statusCode < 300) {
      methodColor = "#16a34a"; // Green
    } else if (sipInfo.statusCode >= 100 && sipInfo.statusCode < 200) {
      methodColor = "#0284c7"; // Blue
    } else if (sipInfo.statusCode >= 300) {
      methodColor = "#dc2626"; // Red
    }
  }

  var headClass = sipInfo.direction === "TX" ? "sip-arrow-head-tx" : "sip-arrow-head-rx";
  var localNodeHtml = sipInfo.direction === "TX" ? sipInfo.src : sipInfo.dst;
  var remoteNodeHtml = sipInfo.direction === "TX" ? sipInfo.dst : sipInfo.src;

  var rowClasses = ["sip-ladder-row"];
  if (missingAck) rowClasses.push("has-missing-ack");
  if (retransmitCount > 0) rowClasses.push("is-retransmit");

  var cseqBadgeHtml = cseqNum !== null
    ? `<span class="sip-cseq-badge" title="CSeq: ${cseqNum} ${escapeHTML(sipInfo.cseqMethod || sipInfo.topText)}">CSeq ${cseqNum}</span>`
    : "";

  var retransmitBadgeHtml = retransmitCount > 0
    ? `<span class="sip-badge sip-badge-retransmit" title="Retransmitted message (count: ${retransmitCount})">🔄 Retransmit #${retransmitCount}</span>`
    : "";

  var missingAckBadgeHtml = missingAck
    ? `<span class="sip-badge sip-badge-missing-ack" title="No ACK received for CSeq ${cseqNum != null ? cseqNum : ""}">⚠️ Missing ACK</span>`
    : "";

  var hasBottomContent = Boolean(sipInfo.bottomText || retransmitCount > 0 || missingAck);

  var html = `
    <div class="${rowClasses.join(" ")}">
      <div class="sip-ladder-header" onclick="this.parentElement.classList.toggle('open')">
        <div class="sip-arrow-container">
          <div class="sip-node sip-node-center" title="${escapeHTML(localNodeHtml)}">
            ${escapeHTML(localNodeHtml)}<br><span style="font-size: 9px; color: #444; font-weight: normal;">${escapeHTML(sipInfo.timeStr)}</span>
          </div>
          <div class="sip-arrow-line">
            <div class="sip-method-top" style="color: ${escapeHTML(methodColor)}">
              <span>(${msgCount}) ${escapeHTML(truncateText(sipInfo.topText, 60))}</span>
              ${cseqBadgeHtml}
            </div>
            ${hasBottomContent ? `
            <div class="sip-method-bottom" style="color: ${escapeHTML(methodColor)}">
              ${sipInfo.bottomText ? `<span>${escapeHTML(truncateText(sipInfo.bottomText, 60))}</span>` : ""}
              ${retransmitBadgeHtml}
              ${missingAckBadgeHtml}
            </div>` : ""}
            <div class="sip-arrow-head ${escapeHTML(headClass)}"></div>
          </div>
          <div class="sip-node sip-node-center" title="${escapeHTML(remoteNodeHtml)}">
            ${escapeHTML(remoteNodeHtml)}<br><span style="font-size: 9px; color: #444; font-weight: normal;">${escapeHTML(sipInfo.timeStr)}</span>
          </div>
        </div>
      </div>
      <pre class="sip-details">${escapeHTML(sipInfo.message)}</pre>
    </div>`;
  return html;
}

export function renderSipEvent(j, elements, getOptions) {
  var sipViewEl = elements.sipViewEl;
  var opts = getOptions ? getOptions() : { autoscroll: true, maxItems: 0 };

  if (j.type !== "SIP" || !sipViewEl) return false;

  var el = document.createElement("div");
  el.className = "sip-ladder-row";

  var param = j.param || "";
  var sipInfo = getSipMessageInfo(param);

  sipViewEl._msgCount = (sipViewEl._msgCount || 0) + 1;

  if (sipInfo.firstLine || sipInfo.method) {
    if (!sipViewEl._sipTracker || sipViewEl._msgCount === 1) {
      if (sipViewEl._sipTracker && sipViewEl._sipTracker.pendingTimers) {
        sipViewEl._sipTracker.pendingTimers.forEach(function (tid) {
          clearTimeout(tid);
        });
      }
      sipViewEl._sipTracker = {
        acks: new Set(),
        sigCounts: new Map(),
        pendingResponses: new Map(),
        pendingTimers: new Map(),
      };
    }
    var tracker = sipViewEl._sipTracker;

    var sig = [
      sipInfo.direction,
      sipInfo.callId,
      sipInfo.cseqNumber,
      sipInfo.cseqMethod,
      sipInfo.statusCode || sipInfo.method,
    ].join("|");
    var count = (tracker.sigCounts.get(sig) || 0) + 1;
    tracker.sigCounts.set(sig, count);
    var retransmitCount = count - 1;

    // Handle incoming ACK
    if (sipInfo.cseqMethod === "ACK" || sipInfo.method === "ACK") {
      if (sipInfo.cseqNumber !== null) {
        var ackKey = (sipInfo.callId || "") + "|" + sipInfo.cseqNumber;
        tracker.acks.add(ackKey);
        if (tracker.pendingTimers.has(ackKey)) {
          clearTimeout(tracker.pendingTimers.get(ackKey));
          tracker.pendingTimers.delete(ackKey);
        }
        if (tracker.pendingResponses.has(ackKey)) {
          var pendingList = tracker.pendingResponses.get(ackKey) || [];
          pendingList.forEach(function (item) {
            item.clearMissingAck();
          });
          tracker.pendingResponses.delete(ackKey);
        }
      }
    }

    var missingAck = false;
    var ackKey = (sipInfo.callId || "") + "|" + sipInfo.cseqNumber;
    if (
      sipInfo.cseqMethod === "INVITE" &&
      sipInfo.isResponse &&
      sipInfo.statusCode >= 200 &&
      sipInfo.cseqNumber !== null
    ) {
      if (tracker.acks.has(ackKey)) {
        missingAck = false;
      } else if (retransmitCount > 0) {
        // Retransmitting final response means no ACK was received!
        missingAck = true;
        var pendingList = tracker.pendingResponses.get(ackKey) || [];
        pendingList.forEach(function (item) {
          item.markMissingAck();
        });
      } else {
        // First occurrence: register pending response and arm 1.5s timer
        var pendingEntry = {
          rowEl: el,
          markMissingAck: function () {
            el.classList.add("has-missing-ack");
            ensureMissingAckBadge(el, sipInfo.cseqNumber);
          },
          clearMissingAck: function () {
            el.classList.remove("has-missing-ack");
            removeMissingAckBadge(el);
          },
        };
        var list = tracker.pendingResponses.get(ackKey) || [];
        list.push(pendingEntry);
        tracker.pendingResponses.set(ackKey, list);

        var timerId = setTimeout(function () {
          tracker.pendingTimers.delete(ackKey);
          if (!tracker.acks.has(ackKey)) {
            var curList = tracker.pendingResponses.get(ackKey) || [];
            curList.forEach(function (item) {
              item.markMissingAck();
            });
          }
        }, 1500);
        tracker.pendingTimers.set(ackKey, timerId);
      }
    }

    if (missingAck) el.classList.add("has-missing-ack");
    if (retransmitCount > 0) el.classList.add("is-retransmit");

    var methodColor = "#000";
    if (sipInfo.isResponse) {
      if (sipInfo.statusCode >= 200 && sipInfo.statusCode < 300) {
        methodColor = "#16a34a"; // Green
      } else if (sipInfo.statusCode >= 100 && sipInfo.statusCode < 200) {
        methodColor = "#0284c7"; // Blue
      } else if (sipInfo.statusCode >= 300) {
        methodColor = "#dc2626"; // Red
      }
    }

    var header = document.createElement("div");
    header.className = "sip-ladder-header";
    header.onclick = function () {
      el.classList.toggle("open");
    };

    var arrowCont = document.createElement("div");
    arrowCont.className = "sip-arrow-container";

    var localNodeEl = document.createElement("div");
    localNodeEl.className = "sip-node sip-node-center";
    var remoteNodeEl = document.createElement("div");
    remoteNodeEl.className = "sip-node sip-node-center";

    var srcNodeHtml =
      escapeHTML(sipInfo.src) +
      '<br><span style="font-size: 9px; color: #444; font-weight: normal;">' +
      escapeHTML(sipInfo.timeStr) +
      "</span>";
    var dstNodeHtml =
      escapeHTML(sipInfo.dst) +
      '<br><span style="font-size: 9px; color: #444; font-weight: normal;">' +
      escapeHTML(sipInfo.timeStr) +
      "</span>";

    if (sipInfo.direction === "TX") {
      localNodeEl.innerHTML = srcNodeHtml;
      localNodeEl.title = sipInfo.src;
      remoteNodeEl.innerHTML = dstNodeHtml;
      remoteNodeEl.title = sipInfo.dst;
    } else {
      localNodeEl.innerHTML = dstNodeHtml;
      localNodeEl.title = sipInfo.dst;
      remoteNodeEl.innerHTML = srcNodeHtml;
      remoteNodeEl.title = sipInfo.src;
    }

    var lineEl = document.createElement("div");
    lineEl.className = "sip-arrow-line";

    var methodTopEl = document.createElement("div");
    methodTopEl.className = "sip-method-top";
    methodTopEl.style.color = methodColor;

    var methodText = document.createElement("span");
    methodText.textContent =
      "(" + sipViewEl._msgCount + ") " + truncateText(sipInfo.topText, 60);
    methodTopEl.appendChild(methodText);

    if (sipInfo.cseqNumber !== null) {
      var cseqBadge = document.createElement("span");
      cseqBadge.className = "sip-cseq-badge";
      cseqBadge.title =
        "CSeq: " +
        sipInfo.cseqNumber +
        (sipInfo.cseqMethod ? " " + sipInfo.cseqMethod : "");
      cseqBadge.textContent = "CSeq " + sipInfo.cseqNumber;
      methodTopEl.appendChild(cseqBadge);
    }

    var actions = document.createElement("div");
    actions.className = "sip-actions";

    var compareBtn = document.createElement("button");
    compareBtn.className = "btn-sip btn-compare";
    compareBtn.textContent = "Compare";
    compareBtn.onclick = function (e) {
      e.stopPropagation();
      var selected = sipViewEl.querySelectorAll(".sip-ladder-row.selected");
      if (el.classList.contains("selected")) {
        el.classList.remove("selected");
        compareBtn.classList.remove("compare-active");
      } else {
        if (selected.length >= 2) {
          var first = selected[0];
          first.classList.remove("selected");
          var firstBtn = first.querySelector(".btn-compare");
          if (firstBtn) firstBtn.classList.remove("compare-active");
        }
        el.classList.add("selected");
        compareBtn.classList.add("compare-active");
      }
      if (window.updateSipCompare) window.updateSipCompare();
    };
    actions.appendChild(compareBtn);
    methodTopEl.appendChild(actions);

    var methodBotEl = null;
    if (sipInfo.bottomText || retransmitCount > 0 || missingAck) {
      methodBotEl = document.createElement("div");
      methodBotEl.className = "sip-method-bottom";
      methodBotEl.style.color = methodColor;

      if (sipInfo.bottomText) {
        var botSpan = document.createElement("span");
        botSpan.textContent = truncateText(sipInfo.bottomText, 60);
        methodBotEl.appendChild(botSpan);
      }
      if (retransmitCount > 0) {
        var retransmitBadge = document.createElement("span");
        retransmitBadge.className = "sip-badge sip-badge-retransmit";
        retransmitBadge.title = "Retransmitted message (count: " + retransmitCount + ")";
        retransmitBadge.textContent = "🔄 Retransmit #" + retransmitCount;
        methodBotEl.appendChild(retransmitBadge);
      }
      if (missingAck) {
        var missingAckBadge = document.createElement("span");
        missingAckBadge.className = "sip-badge sip-badge-missing-ack";
        missingAckBadge.title =
          "No ACK received for CSeq " +
          (sipInfo.cseqNumber != null ? sipInfo.cseqNumber : "");
        missingAckBadge.textContent = "⚠️ Missing ACK";
        methodBotEl.appendChild(missingAckBadge);
      }
    }

    var headEl = document.createElement("div");
    headEl.className =
      "sip-arrow-head " +
      (sipInfo.direction === "TX" ? "sip-arrow-head-tx" : "sip-arrow-head-rx");

    lineEl.appendChild(methodTopEl);
    if (methodBotEl) lineEl.appendChild(methodBotEl);
    lineEl.appendChild(headEl);

    arrowCont.appendChild(localNodeEl);
    arrowCont.appendChild(lineEl);
    arrowCont.appendChild(remoteNodeEl);

    header.appendChild(arrowCont);

    var details = document.createElement("pre");
    details.className = "sip-details";
    details.textContent = sipInfo.message;

    el.appendChild(header);
    el.appendChild(details);
  } else {
    el.textContent = param;
    el.style.padding = "10px";
  }

  // Store unformatted text for comparison
  el._sipData = {
    diffKey: getSipDiffKey(param),
    method: sipInfo.label || sipInfo.topText,
    raw: sipInfo.message || param,
    seq: sipViewEl._msgCount,
  };

  appendAndMaintain(sipViewEl, el, opts);
  return true; // handled
}

export function initSipCompare(elements) {
  const { sipViewEl, sipComparePanel, closeSipCompareBtn } = elements;

  const compareLeftRoot = document.getElementById("sip-compare-left");
  const compareRightRoot = document.getElementById("sip-compare-right");
  const compareLeftHeader =
    compareLeftRoot?.querySelector(".sip-compare-header");
  const compareRightHeader =
    compareRightRoot?.querySelector(".sip-compare-header");
  const compareLeftContent = document.getElementById(
    "sip-compare-content-left",
  );
  const compareRightContent = document.getElementById(
    "sip-compare-content-right",
  );

  if (closeSipCompareBtn) {
    closeSipCompareBtn.onclick = () => {
      if (sipViewEl) {
        const selected = sipViewEl.querySelectorAll(".sip-ladder-row.selected");
        selected.forEach((el) => {
          el.classList.remove("selected");
          const btn = el.querySelector(".btn-compare");
          if (btn) btn.classList.remove("compare-active");
        });
      }
      if (sipComparePanel) sipComparePanel.style.display = "none";
    };
  }

  window.updateSipCompare = () => {
    if (!sipViewEl || !sipComparePanel) return;
    const selected = sipViewEl.querySelectorAll(".sip-ladder-row.selected");
    sipComparePanel.style.display = selected.length ? "flex" : "none";
    if (!selected.length) return;

    const leftData = selected[0]?._sipData;
    const rightData = selected[1]?._sipData;

    if (compareLeftHeader) {
      compareLeftHeader.textContent = leftData
        ? "(" + leftData.seq + ") " + leftData.method
        : "";
    }
    if (compareRightHeader) {
      compareRightHeader.textContent = rightData
        ? "(" + rightData.seq + ") " + rightData.method
        : "";
    }

    if (compareLeftContent) {
      compareLeftContent.textContent = leftData ? leftData.raw : "";
      if (compareLeftContent.parentElement) {
        compareLeftContent.parentElement.style.display = leftData
          ? "flex"
          : "none";
      }
    }

    if (compareRightContent) {
      compareRightContent.textContent = rightData ? rightData.raw : "";
      if (compareRightContent.parentElement) {
        compareRightContent.parentElement.style.display = rightData
          ? "flex"
          : "none";
      }
    }
  };
}


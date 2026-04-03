import {
  safeText,
  trimChildrenToMax,
  appendAndMaintain,
} from "./utils.js";

function truncateText(text, maxLen) {
  var s = safeText(text);
  return s.length > maxLen ? s.substring(0, maxLen) + "..." : s;
}

export function renderSipEvent(j, elements, getOptions) {
  var sipViewEl = elements.sipViewEl;
  var opts = getOptions ? getOptions() : { autoscroll: true, maxItems: 0 };

  if (j.type !== "SIP" || !sipViewEl) return false;

  var el = document.createElement("div");
  el.className = "sip-ladder-row";

  var param = j.param || "";
  var lines = param.split("\n");
  var rest = "";
  var topText = "";

  sipViewEl._msgCount = (sipViewEl._msgCount || 0) + 1;

  if (lines.length >= 2) {
    var fullTimeStr = safeText(lines[0].replace("#", ""));
    var parts = fullTimeStr.split("|");
    var timeStr = parts[0];
    var dir = parts.length > 1 ? parts[1] : "TX";

    var arrowMatch = safeText(lines[1]).match(
      /(\w+) ([0-9\.\:a-fA-F\[\]]+) -> ([0-9\.\:a-fA-F\[\]]+)/,
    );
    rest = safeText(lines.slice(2).join("\n"));
    var methodLine = safeText(lines[2] || "");

    topText = methodLine;
    var bottomText = "";

    // Extract method from CSeq
    var cseqMatch = rest.match(/CSeq:\s*\d+\s+([A-Z]+)/);
    if (cseqMatch) {
      topText = cseqMatch[1];
      if (rest.startsWith("SIP/2.0")) {
        var statusCode = rest.match(/SIP\/2\.0\s+(\d+)\s+(.*)/);
        if (statusCode) {
          bottomText = statusCode[1] + " " + statusCode[2];
        }
      }
    } else if (!arrowMatch) {
      topText = safeText(lines[1]);
    }

    var src = "Unknown",
      dst = "Unknown";
    if (arrowMatch) {
      src = arrowMatch[2];
      dst = arrowMatch[3];
    }

    var isResponse = rest.startsWith("SIP/2.0");
    var methodColor = isResponse
      ? rest.includes(" 200")
        ? "#16a34a"
        : rest.includes(" 100") ||
            rest.includes(" 180") ||
            rest.includes(" 183")
          ? "#0284c7"
          : "#dc2626"
      : "#000";

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
      src +
      '<br><span style="font-size: 9px; color: #444; font-weight: normal;">' +
      timeStr +
      "</span>";
    var dstNodeHtml =
      dst +
      '<br><span style="font-size: 9px; color: #444; font-weight: normal;">' +
      timeStr +
      "</span>";

    if (dir === "TX") {
      localNodeEl.innerHTML = srcNodeHtml;
      localNodeEl.title = src;
      remoteNodeEl.innerHTML = dstNodeHtml;
      remoteNodeEl.title = dst;
    } else {
      localNodeEl.innerHTML = dstNodeHtml;
      localNodeEl.title = dst;
      remoteNodeEl.innerHTML = srcNodeHtml;
      remoteNodeEl.title = src;
    }

    var lineEl = document.createElement("div");
    lineEl.className = "sip-arrow-line";

    var methodTopEl = document.createElement("div");
    methodTopEl.className = "sip-method-top";
    methodTopEl.style.color = methodColor;

    var methodText = document.createElement("span");
    methodText.textContent =
      "(" + sipViewEl._msgCount + ") " + truncateText(topText, 60);
    methodTopEl.appendChild(methodText);

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
    if (bottomText) {
      methodBotEl = document.createElement("div");
      methodBotEl.className = "sip-method-bottom";
      methodBotEl.style.color = methodColor;
      methodBotEl.textContent = truncateText(bottomText, 60);
    }

    var headEl = document.createElement("div");
    headEl.className =
      "sip-arrow-head " +
      (dir === "TX" ? "sip-arrow-head-tx" : "sip-arrow-head-rx");

    lineEl.appendChild(methodTopEl);
    if (methodBotEl) lineEl.appendChild(methodBotEl);
    lineEl.appendChild(headEl);

    arrowCont.appendChild(localNodeEl);
    arrowCont.appendChild(lineEl);
    arrowCont.appendChild(remoteNodeEl);

    header.appendChild(arrowCont);

    var details = document.createElement("pre");
    details.className = "sip-details";
    details.textContent = rest;

    el.appendChild(header);
    el.appendChild(details);
  } else {
    el.textContent = param;
    el.style.padding = "10px";
    topText = param;
    rest = param;
  }

  // Store unformatted text for comparison
  el._sipData = {
    raw: rest,
    method: topText,
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
    compareLeftRoot?.querySelector(".sip-ladder-header");
  const compareRightHeader =
    compareRightRoot?.querySelector(".sip-ladder-header");
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


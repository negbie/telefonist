function normalizeEvent(j) {
  if (!j || typeof j !== "object") return null;

  if (!Object.prototype.hasOwnProperty.call(j, "time")) {
    j.time = nowLocalString();
  }

  // Most stripping and normalization is now done in Go
  return j;
}

function formatJSON(j) {
  var dataLines = null;
  if (j && j.data != null) {
    dataLines = String(j.data)
      .split(/\r?\n\s*\r?\n|[\r\n]+/)
      .filter(Boolean);
  }

  var text = "";
  for (var key in j) {
    if (!Object.prototype.hasOwnProperty.call(j, key)) continue;
    if (key === "event" || key === "time") continue; // Redundant in display

    var val = key === "data" && dataLines !== null ? dataLines : j[key];
    if (typeof val === "object") {
      try {
        val = JSON.stringify(val, null, 2);
      } catch (e) {
        val = String(val);
      }
    } else {
      val = String(val);
    }

    var prettyKey = key.charAt(0).toUpperCase() + key.slice(1);
    text += prettyKey + ": " + val + "\n";
  }
  return text.trim() || "{}";
}

function getSummaryFields(j) {
  var token = safeText(j.token || "");
  var type = safeText(j.type || "");

  // Defaults (calls/events)
  var direction = safeText(j.direction || "");
  var peer = safeText(j.peeruri || j.contacturi || j.param || "");
  var cuser = safeText(j.cuser || "");

  // For test/testfile, show:
  //   - direction column: status (running/progress/finished)
  var status = safeText(j.status || "");
  var result = safeText(j.result || "");

  // Additionally for testfile, show the filename in the 3rd column on `running` and `finished`
  // messages (they include "file"). For per-test progress rows, show the test case name
  // (instead of the command sequence) in the 3rd column.
  var file = safeText(j.file || "");
  var name = safeText(j.name || "");

  if (token === "test" || token === "testfile") {
    if (status) direction = status;

    if (
      token === "testfile" &&
      file &&
      (status === "running" || status === "finished")
    ) {
      peer = file;
    } else if (token === "testfile" && name && result) {
      // Per-test progress row: show "PASS|FAIL — <name>".
      peer = result + " — " + name;
    } else if (token === "testfile" && name) {
      // If result is missing, still show the named test case.
      peer = name;
    } else if (result) {
      peer = result;
    } else if (file) {
      // Fallback: if we have file but no status match (or status missing), show it.
      peer = file;
    } else {
      peer = "";
    }
  }

  return {
    time: safeText(j.time || ""),
    type: type,
    token: token,
    peer: peer,
  };
}

function createSequentialFlowRenderer(flowEl, getOptions) {
  function setDetailsOpenState(detailsEl, open) {
    if (open) detailsEl.setAttribute("open", "open");
    else detailsEl.removeAttribute("open");
  }

  function setPeerClasses(peerSpan) {
    // Color PASS/FAIL in CSS via .pass/.fail classes.
    var text = (peerSpan.textContent || "").trim();
    peerSpan.classList.remove("pass");
    peerSpan.classList.remove("fail");
    if (text.indexOf("PASS") === 0) peerSpan.classList.add("pass");
    else if (text.indexOf("FAIL") === 0) peerSpan.classList.add("fail");
  }

  function addEventCard(j) {
    var f = getSummaryFields(j);
    var opts = getOptions
      ? getOptions()
      : { autoscroll: true, maxItems: 0, collapseAll: false };

    var doScroll = opts.autoscroll || isNearBottom(flowEl);

    var eventDetails = document.createElement("details");
    eventDetails.className = "event list";
    tagToken(eventDetails, f.token);
    if (j.result) {
      eventDetails.setAttribute("data-has-result", "1");
    }
    setDetailsOpenState(eventDetails, !opts.collapseAll);

    var eventSummary = document.createElement("summary");

    var tTime = ensureElement(eventSummary, "span", "evt-time");
    tTime.textContent = f.time;

    var tType = ensureElement(eventSummary, "span", "evt-type");
    tType.textContent = f.type || f.token || "(no type)";

    var tPeer = ensureElement(eventSummary, "span", "evt-peer");
    if (f.peer) tPeer.textContent = f.peer;
    else tPeer.textContent = "";

    setPeerClasses(tPeer);

    eventDetails.appendChild(eventSummary);

    var jsonPre = ensureElement(eventDetails, "pre", "evt-json");
    if (j._details) {
      jsonPre.textContent = j._details;
    } else {
      jsonPre.textContent = formatJSON(j);
    }

    var searchInput = document.getElementById("search");
    if (searchInput && searchInput.value) {
      var filter = searchInput.value.toLowerCase();
      if (!eventDetails.textContent.toLowerCase().includes(filter)) {
        eventDetails.style.display = "none";
      }
    }

    flowEl.appendChild(eventDetails);
    trimChildrenToMax(flowEl, opts.maxItems);

    if (doScroll && eventDetails.style.display !== "none") {
      scrollToBottom(flowEl);
    }
  }

  function setCollapseAll(collapse) {
    var nodes = flowEl ? flowEl.querySelectorAll("details.event") : [];
    for (var i = 0; i < nodes.length; i++) {
      if (collapse) nodes[i].removeAttribute("open");
      else nodes[i].setAttribute("open", "open");
    }
  }

  return {
    renderEvent: addEventCard,
    setCollapseAll: setCollapseAll,
  };
}

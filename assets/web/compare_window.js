import { EventBus } from "./event_bus.js";
import { escapeHTML, base64DecodeUTF8 } from "./utils.js";
import { computeRunDiff } from "./diff.js";
import { loadLogFilters, shouldFilterLog } from "./log_filter_manager.js";
import {
  analyzeSipEvents,
  getSipDiffKey,
  getSipMessageInfo,
} from "./sip_renderer.js";

export const ignoredEvtTypes = new Set([
  "sip",
  "log",
  "testfile",
  "module",
  "response",
  "native_done",
  "shutdown",
  "exit",
  "end_of_file",
]);

export function initCompareWindow(deps) {
  const { getActiveKey } = deps;
  const selectA = document.getElementById("compare-run-a");
  const selectB = document.getElementById("compare-run-b");
  const contentA = document.getElementById("compare-content-a");
  const contentB = document.getElementById("compare-content-b");
  const headerA = document.getElementById("compare-header-a");
  const headerB = document.getElementById("compare-header-b");
  const clearBtn = document.getElementById("compare-clear");
  const swapBtn = document.getElementById("compare-swap");
  const deleteSelectedBtn = document.getElementById("compare-delete-selected");
  const diffBtn = document.getElementById("compare-diff");
  const evtBtn = document.getElementById("compare-evt");
  const sipBtn = document.getElementById("compare-sip");
  const logBtn = document.getElementById("compare-log");
  const wavBtn = document.getElementById("compare-wav");
  const deleteAllBtn = document.getElementById("compare-delete-all");
  const linkBtn = document.getElementById("compare-link");

  let isScrollLinked = true;
  let isSyncing = false;

  const handleScrollA = () => {
    if (!isScrollLinked || isSyncing || !contentA || !contentB) return;
    isSyncing = true;
    const maxA = contentA.scrollHeight - contentA.clientHeight;
    if (maxA > 0) {
      const pct = contentA.scrollTop / maxA;
      const maxB = contentB.scrollHeight - contentB.clientHeight;
      contentB.scrollTop = Math.round(pct * maxB);
    }
    setTimeout(() => { isSyncing = false; }, 10);
  };

  const handleScrollB = () => {
    if (!isScrollLinked || isSyncing || !contentA || !contentB) return;
    isSyncing = true;
    const maxB = contentB.scrollHeight - contentB.clientHeight;
    if (maxB > 0) {
      const pct = contentB.scrollTop / maxB;
      const maxA = contentA.scrollHeight - contentA.clientHeight;
      contentA.scrollTop = Math.round(pct * maxA);
    }
    setTimeout(() => { isSyncing = false; }, 10);
  };

  if (contentA) contentA.addEventListener("scroll", handleScrollA);
  if (contentB) contentB.addEventListener("scroll", handleScrollB);

  if (linkBtn) {
    linkBtn.onclick = () => {
      isScrollLinked = !isScrollLinked;
      if (isScrollLinked) {
        linkBtn.classList.add("active");
        linkBtn.textContent = "Linked scroll";
        handleScrollA();
      } else {
        linkBtn.classList.remove("active");
        linkBtn.textContent = "Independent scroll";
      }
    };
  }

  const comparePanel = document.getElementById("compare-panel");
  if (comparePanel) {
    comparePanel.addEventListener("click", (e) => {
      // Recalculate row heights after an event or SIP detail is expanded.
      setTimeout(() => schedulePairAlignment(), 0);

      const seq = e.target.closest(".evt-seq");
      if (!seq) return;

      e.preventDefault();
      e.stopPropagation();

      const sourceRow = seq.closest(".compare-pair-row");
      const pair = sourceRow?.getAttribute("data-pair");
      const side = seq.closest("#compare-content-a") ? "a" : "b";
      const targetContent = side === "a" ? contentB : contentA;
      const target = pair == null ? null : targetContent?.querySelector(`.compare-pair-row[data-pair="${pair}"]`);
      if (target) {
        target.scrollIntoView({ behavior: "smooth", block: "center" });
        if (typeof target.animate === "function") {
          target.animate(
            [{ boxShadow: "0 0 0 2px #facc15" }, { boxShadow: "none" }],
            { duration: 900 },
          );
        }
      }
    });
  }

  let currentTestfile = "";
  let currentProject = "";
  let rawA = "";
  let rawB = "";
  let wavsA = [];
  let wavsB = [];
  // Show the complete flow by default; users can narrow it to differences.
  let diffMode = false;
  let activeMode = "evt"; // evt, sip, log, wav

  const envelopeFields = new Set([
    "time", "timestamp", "elapsed_ms", "event_sequence", "schema_version",
    "id", "RawJSON", "run_id", "idx", "_seq", "_epoch",
  ]);

  const stableStringify = value => {
    if (Array.isArray(value)) return `[${value.map(stableStringify).join(",")}]`;
    if (value && typeof value === "object") {
      return `{${Object.keys(value).sort().map(key => `${JSON.stringify(key)}:${stableStringify(value[key])}`).join(",")}}`;
    }
    return JSON.stringify(value);
  };

  const displayValue = value => typeof value === "object" ? stableStringify(value) : String(value);

  const renderEventHTML = (j, changedFields = null) => {
    const type = (j.type || j.token || "EVENT").toUpperCase();
    const content = Object.entries(j)
      .sort(([a], [b]) => a.localeCompare(b))
      .filter(([k, v]) => v != null && v !== "" && !envelopeFields.has(k) && !["type", "token", "class", "event"].includes(k))
      .map(([k, v]) => {
        const pk = k.charAt(0).toUpperCase() + k.slice(1);
        const changedClass = changedFields?.has(k) ? " is-field-changed" : "";
        return `<div class="json-row${changedClass}"><span class="json-key">${escapeHTML(pk)}</span>: <span class="json-value">${escapeHTML(displayValue(v))}</span></div>`;
      })
      .join("");

    return `
<details class="event list" open>
    <summary>
        <span class="evt-seq" data-idx="${j.idx}" title="Original position">${j.idx + 1}</span>
        <span class="evt-type">${escapeHTML(type)}</span>
    </summary>
    <div class="evt-content">${content}</div>
</details>`.trim();
  };

  // Capture metadata should not make two otherwise equal records look different.
  // Protocol values remain untouched so real header, address and identifier changes
  // are still visible after matching has found the correct counterpart.
  const structuralSkeleton = text => String(text || "").replace(/\r\n/g, "\n").trim();
  const normalizeLog = text => structuralSkeleton(text).split("\n").map(line => line
    .replace(/^\s*\[?\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?\]?\s*(?:[-|]\s*)?/, "")
    .replace(/^\s*\[?\d{2}:\d{2}:\d{2}(?:[.,]\d+)?\]?\s*(?:[-|]\s*)?/, "")
    .replace(/^\s*\[?\d{10}(?:\.\d+)?\]?\s*(?:[-|]\s*)?/, ""),
  ).join("\n").trim();

  const normalizedEvent = event => {
    const clean = {};
    Object.entries(event).forEach(([key, value]) => {
      if (!envelopeFields.has(key)) clean[key] = typeof value === "string" ? structuralSkeleton(value) : value;
    });
    return clean;
  };

  const sipHost = endpoint => {
    const value = String(endpoint || "Unknown").trim();
    const bracketed = value.match(/^\[([^\]]+)\](?::\d+)?$/);
    if (bracketed) return bracketed[1];
    const hostAndPort = value.match(/^([^:]+):(\d+)$/);
    return hostAndPort ? hostAndPort[1] : value;
  };

  const renderSipFlowHTML = (info, seq, meta, rawMessage) => {
    const direction = info.direction === "RX" ? "RX" : "TX";
    const srcHost = sipHost(info.src);
    const dstHost = sipHost(info.dst);
    const localHost = direction === "TX" ? srcHost : dstHost;
    const remoteHost = direction === "TX" ? dstHost : srcHost;
    const method = info.cseqMethod || info.method || "SIP";
    const label = info.isResponse
      ? `${info.statusCode}${info.statusText ? ` ${info.statusText}` : ""}`
      : method;
    const kind = info.isResponse ? `response to ${method}` : "request";
    const notes = [
      `${direction} · ${kind}`,
      meta.retransmitCount > 0 ? `retransmit ${meta.retransmitCount}` : "",
      meta.missingAck ? "missing ACK" : "",
    ].filter(Boolean).join(" · ");
    const statusClass = info.isResponse
      ? (info.statusCode >= 200 && info.statusCode < 300 ? " is-success" : info.statusCode >= 300 ? " is-error" : " is-response")
      : "";

    return `
      <details class="sip-flow-details">
        <summary title="Expand full SIP message">
          <div class="sip-flow-card">
            <div class="sip-flow-host">${escapeHTML(localHost)}</div>
            <div class="sip-flow-center">
              <div class="sip-flow-title${statusClass}"><span>${seq}</span>${escapeHTML(label)}</div>
              <div class="sip-flow-route">${direction === "TX" ? "⟶" : "⟵"}</div>
              <div class="sip-flow-meta">${escapeHTML(notes)}</div>
            </div>
            <div class="sip-flow-host">${escapeHTML(remoteHost)}</div>
          </div>
        </summary>
        <pre>${escapeHTML(rawMessage)}</pre>
      </details>`;
  };

  const parseEvents = (text) => {
    const rawEvents = (text || "").split(/\n\n+/).map(s => s.trim()).filter(Boolean).map(line => {
      try { return JSON.parse(line); } catch (e) { return null; }
    }).filter(Boolean).filter(j => {
      const type = (j.type || j.token || "").toLowerCase();
      if (activeMode === "sip") return type === "sip";
      if (activeMode === "log") return type === "log" && !shouldFilterLog(j.param);
      return activeMode !== "evt" || !ignoredEvtTypes.has(type);
    });

    const sipMetas = activeMode === "sip" ? analyzeSipEvents(rawEvents) : [];

    return rawEvents.map((j, idx) => {
      if (activeMode === "sip") {
        const sipInfo = getSipMessageInfo(j.param || "");
        const seq = idx + 1;
        const meta = sipMetas[idx] || {};
        const rawMessage = sipInfo.message || j.param || "";
        const method = sipInfo.cseqMethod || sipInfo.method || getSipDiffKey(j.param || "") || "SIP";
        const flow = {
          direction: sipInfo.direction,
          from: sipHost(sipInfo.src),
          to: sipHost(sipInfo.dst),
          kind: sipInfo.isResponse ? "response" : "request",
          method,
          status: sipInfo.statusCode || 0,
          missingAck: Boolean(meta.missingAck),
        };
        const compare = stableStringify(flow);
        return {
          compare,
          display: renderSipFlowHTML(sipInfo, seq, meta, rawMessage),
          flow,
          idx,
          matchKey: `sip:${flow.direction}:${flow.kind}:${flow.method}`,
          matchThreshold: 0.55,
        };
      }

      const display = renderEventHTML({ ...j, idx });
      if (activeMode === "log") {
        const message = normalizeLog(j.param || "");
        const agent = structuralSkeleton(j._agent || j.agent || "").toLowerCase();
        const logger = message.match(/^([a-z0-9_.-]+)\s*:/i)?.[1]?.toLowerCase() || "";
        return {
          compare: stableStringify({ agent, message }),
          display,
          idx,
          matchKey: `log:${agent}:${logger}`,
          matchThreshold: 0.45,
          event: j,
        };
      }

      const clean = normalizedEvent(j);
      const type = String(j.type || j.token || "event").toLowerCase();
      const eventClass = String(j.class || j.event || "").toLowerCase();
      return {
        compare: stableStringify(clean),
        display,
        idx,
        matchKey: `event:${type}:${eventClass}`,
        matchThreshold: 0.34,
        event: j,
      };
    });
  };

  let alignFrame = 0;
  const alignPairRows = () => {
    if (!contentA || !contentB) return;
    const rowsA = [...contentA.querySelectorAll(".compare-pair-row")];
    const rowsB = [...contentB.querySelectorAll(".compare-pair-row")];
    rowsA.forEach(row => { row.style.minHeight = ""; });
    rowsB.forEach(row => { row.style.minHeight = ""; });
    rowsA.forEach((rowA, index) => {
      const rowB = rowsB[index];
      if (!rowB) return;
      const height = Math.max(rowA.offsetHeight, rowB.offsetHeight);
      rowA.style.minHeight = `${height}px`;
      rowB.style.minHeight = `${height}px`;
    });
  };
  const schedulePairAlignment = () => {
    cancelAnimationFrame(alignFrame);
    alignFrame = requestAnimationFrame(alignPairRows);
  };

  const emptyState = message => `<div class="compare-empty-state">${escapeHTML(message)}</div>`;
  const pairPlaceholder = message => `<div class="compare-pair-placeholder">${escapeHTML(message)}</div>`;
  const changedEventFields = (left, right) => {
    const keys = new Set([...Object.keys(left || {}), ...Object.keys(right || {})]);
    envelopeFields.forEach(key => keys.delete(key));
    keys.delete("type");
    keys.delete("token");
    keys.delete("class");
    keys.delete("event");
    return new Set([...keys].filter(key => stableStringify(left?.[key]) !== stableStringify(right?.[key])));
  };
  const rowHTML = (item, pair, state, label, changedFields = null) => {
    const display = item?.event
      ? renderEventHTML({ ...item.event, idx: item.idx }, changedFields)
      : item?.display;
    return `
    <div class="compare-pair-row ${state}" data-pair="${pair}">
      ${label ? `<div class="compare-change-label">${escapeHTML(label)}</div>` : ""}
      ${display || pairPlaceholder("No matching item")}
    </div>`;
  };

  const renderSummary = (diff, totalA, totalB) => {
    const counts = { common: 0, changed: 0, a: 0, b: 0 };
    diff.forEach(entry => counts[entry.type]++);
    const changed = counts.changed + counts.a + counts.b;
    if (diffBtn && activeMode !== "wav") {
      diffBtn.textContent = diffMode ? `Show all (${counts.common})` : "Only changes";
      diffBtn.title = diffMode
        ? `Return to the complete flow (${counts.common} unchanged item(s))`
        : "Hide unchanged items";
    }
    const note = document.getElementById("compare-note");
    if (!note) return;
    note.classList.toggle("is-identical", changed === 0);
    const comparisonHelp = activeMode === "sip"
      ? "Message bodies and timestamps are ignored."
      : "Order, timestamps and run metadata are ignored.";
    if (changed === 0) {
      note.innerHTML = `<span class="compare-summary-strong">Runs match</span><span class="compare-summary-part">${counts.common} unchanged</span><span class="compare-summary-help">${comparisonHelp}</span>`;
      return;
    }
    note.innerHTML = `<span class="compare-summary-strong">${changed} difference${changed === 1 ? "" : "s"}</span>` +
      `<span class="compare-summary-part">${counts.changed} changed</span>` +
      `<span class="compare-summary-part">${counts.a} baseline only</span>` +
      `<span class="compare-summary-part">${counts.b} current only</span>` +
      `<span class="compare-summary-part">${counts.common} unchanged</span>` +
      `<span class="compare-summary-help">Compared ${totalA} to ${totalB}. ${comparisonHelp}</span>`;
  };

  let diffWorker = null, generation = 0;
  const renderDiff = async () => {
    const current = generation;
    if (contentA) contentA.dataset.renderedMode = activeMode;
    if (contentB) contentB.dataset.renderedMode = activeMode;
    if (!rawA || !rawB) {
      const message = !rawA && !rawB ? "Choose a baseline and a run to inspect." : "Choose the other run to start comparing.";
      contentA.innerHTML = emptyState(message);
      contentB.innerHTML = emptyState(message);
      const note = document.getElementById("compare-note");
      if (note) {
        note.classList.remove("is-identical");
        note.textContent = message;
      }
      return;
    }

    const aItems = parseEvents(rawA), bItems = parseEvents(rawB);
    let diff;
    if (typeof Worker === "function") {
      diffWorker = new Worker(new URL("./diff_worker.js", import.meta.url), { type: "module" });
      try {
        diff = await new Promise((resolve, reject) => {
          diffWorker.onmessage = event => resolve(event.data);
          diffWorker.onerror = reject;
          diffWorker.postMessage({ a: aItems, b: bItems });
        });
      } catch (_) {
        if (current === generation) {
          contentA.innerHTML = emptyState("Comparison failed.");
          contentB.innerHTML = emptyState("Comparison failed.");
        }
        return;
      }
      if (current !== generation) return;
      diffWorker.terminate();
      diffWorker = null;
    } else {
      diff = computeRunDiff(aItems, bItems);
    }

    renderSummary(diff, aItems.length, bItems.length);
    const visible = diffMode ? diff.filter(entry => entry.type !== "common") : diff;
    if (!visible.length) {
      const message = diffMode ? "No differences in this category." : "No items in this category.";
      contentA.innerHTML = emptyState(message);
      contentB.innerHTML = emptyState(message);
      return;
    }

    const aParts = [];
    const bParts = [];
    visible.forEach((entry, pair) => {
      if (entry.type === "common") {
        aParts.push(rowHTML(entry.itemA, pair, "is-common", ""));
        bParts.push(rowHTML(entry.itemB, pair, "is-common", ""));
      } else if (entry.type === "changed") {
        const changedFields = entry.itemA?.event && entry.itemB?.event
          ? changedEventFields(entry.itemA.event, entry.itemB.event)
          : null;
        const label = entry.itemA?.flow ? "Flow changed" : "Changed";
        aParts.push(rowHTML(entry.itemA, pair, "is-changed", label, changedFields));
        bParts.push(rowHTML(entry.itemB, pair, "is-changed", label, changedFields));
      } else if (entry.type === "a") {
        aParts.push(rowHTML(entry.itemA, pair, "is-only-a", "Baseline only"));
        bParts.push(rowHTML(null, pair, "is-only-a", ""));
      } else {
        aParts.push(rowHTML(null, pair, "is-only-b", ""));
        bParts.push(rowHTML(entry.itemB, pair, "is-only-b", "Current only"));
      }
    });
    contentA.innerHTML = aParts.join("");
    contentB.innerHTML = bParts.join("");
    schedulePairAlignment();
  };

  const applyCurrentMode = () => {
    generation++;
    if (diffWorker) { diffWorker.terminate(); diffWorker = null; }
    if (!contentA || !contentB) return;
    contentA.scrollTop = 0;
    contentB.scrollTop = 0;
    if (activeMode === "wav") {
      renderWavs(contentA, wavsA, "a");
      renderWavs(contentB, wavsB, "b");
      const note = document.getElementById("compare-note");
      if (note) {
        note.classList.remove("is-identical");
        note.textContent = "Audio recordings are listed by run; use the players and waveforms to inspect them.";
      }
    } else {
      renderDiff();
    }
    [evtBtn, sipBtn, logBtn, wavBtn].forEach(b => {
      const active = b?.id === `compare-${activeMode}`;
      b?.classList.toggle("active", active);
      b?.setAttribute("aria-selected", String(active));
    });
    if (diffBtn) {
      diffBtn.hidden = activeMode === "wav";
      diffBtn.classList.toggle("active", diffMode);
      diffBtn.textContent = diffMode ? "Show all" : "Only changes";
      diffBtn.title = diffMode ? "Include unchanged items" : "Hide unchanged items";
    }
  };

  const wavAbortControllers = { a: null, b: null };
  let wavsRunA = null;
  let wavsRunB = null;

  const renderWavs = (el, wavs, side) => {
    if (!el) return;
    const currentRunId = side === "a" ? selectA?.value : selectB?.value;
    if (!currentRunId || !wavs?.length) {
      if (wavAbortControllers[side]) {
        wavAbortControllers[side].abort();
        wavAbortControllers[side] = null;
      }
      el.dataset.renderedMode = "wav";
      el.dataset.renderedRun = "";
      el.innerHTML = "<div class='wav-empty'>No recordings.</div>";
      return;
    }
    if (el.dataset.renderedMode === "wav" && el.dataset.renderedRun === String(currentRunId)) {
      return;
    }
    el.dataset.renderedMode = "wav";
    el.dataset.renderedRun = String(currentRunId);

    if (wavAbortControllers[side]) {
      wavAbortControllers[side].abort();
    }
    const abortCtrl = new AbortController();
    wavAbortControllers[side] = abortCtrl;

    el.innerHTML = `<div class='wav-list'>${wavs.map(w => `
      <div class='wav-item'>
        <div class='wav-info'>${escapeHTML(w.filename)}</div>
        <canvas id='canvas-wav-${side}-${w.id}' class='wav-waveform'></canvas>
        <audio id='audio-wav-${side}-${w.id}' controls preload='none' src='/api/testrun/wav?id=${w.id}'></audio>
      </div>`).join("")}</div>`;

    wavs.forEach(w => {
      const canvas = document.getElementById(`canvas-wav-${side}-${w.id}`);
      const audio = document.getElementById(`audio-wav-${side}-${w.id}`);
      if (canvas && audio) {
        drawWaveform(canvas, `/api/testrun/wav?id=${w.id}`, audio, abortCtrl.signal);
      }
    });
  };

  const drawWaveform = async (canvas, url, audioEl, signal) => {
    const ctx = canvas.getContext("2d");
    const { offsetWidth: width = 400, offsetHeight: height = 120 } = canvas;
    canvas.width = width; canvas.height = height;

    const draw = (data, progress = 0) => {
      ctx.fillStyle = "#ffffff"; ctx.fillRect(0, 0, width, height);
      if (!data) return;
      const step = Math.ceil(data.length / width), amp = height / 2;
      ctx.fillStyle = "#3b82f6";
      for (let i = 0; i < width; i++) {
        let min = 1, max = -1;
        for (let j = 0; j < step; j++) {
          const d = data[i * step + j];
          if (d < min) min = d; if (d > max) max = d;
        }
        ctx.fillRect(i, (1 + min) * amp, 1, Math.max(1, (max - min) * amp));
      }
      ctx.fillStyle = "#ef4444"; ctx.fillRect(progress * width, 0, 2, height);
    };

    draw(null); ctx.fillStyle = "#3b82f6"; ctx.fillText("Loading...", 10, 20);
    try {
      const resp = await fetch(url, { signal }), buffer = await resp.arrayBuffer();
      if (signal?.aborted) return;
      const AudioCtx = window.AudioContext || window.webkitAudioContext;
      if (!AudioCtx) return;
      const audioCtx = new AudioCtx();
      let audioBuffer;
      try {
        audioBuffer = await audioCtx.decodeAudioData(buffer);
      } finally {
        if (typeof audioCtx.close === "function") {
          audioCtx.close().catch(() => {});
        }
      }
      if (signal?.aborted) return;
      const rawData = audioBuffer.getChannelData(0);
      draw(rawData);
      audioEl.ontimeupdate = () => draw(rawData, audioEl.currentTime / audioEl.duration);
    } catch (e) {
      if (signal?.aborted || e?.name === "AbortError") return;
      ctx.fillStyle = "#ef4444"; ctx.fillText("Error", 10, 20);
    }
  };

  const fetchWavs = (side, id) => {
    if (!id) {
      if (side === "a") { wavsA = []; wavsRunA = null; }
      else { wavsB = []; wavsRunB = null; }
      if (activeMode === "wav") applyCurrentMode();
      return;
    }
    const selected = () => String(side === "a" ? selectA?.value : selectB?.value) === String(id);
    fetch(`/api/testrun/wavs?id=${id}`)
      .then((r) => r.json())
      .then((wj) => {
        if (!selected() || wj.status !== "finished") return;
        const items = wj.items || [];
        items.sort((a, b) => {
          const tsA = (a.filename.match(/\d{4}-\d{2}-\d{2}-\d{2}-\d{2}-\d{2}/) || [""])[0];
          const tsB = (b.filename.match(/\d{4}-\d{2}-\d{2}-\d{2}-\d{2}/) || [""])[0];
          return tsA.localeCompare(tsB) || a.filename.localeCompare(b.filename);
        });
        if (side === "a") { wavsA = items; wavsRunA = String(id); }
        else { wavsB = items; wavsRunB = String(id); }
        if (activeMode === "wav") applyCurrentMode();
      })
      .catch(() => {
        if (!selected()) return;
        if (side === "a") { wavsA = []; wavsRunA = null; }
        else { wavsB = []; wavsRunB = null; }
        if (activeMode === "wav") applyCurrentMode();
      });
  };

  const clearSide = (side) => {
    if (wavAbortControllers[side]) {
      wavAbortControllers[side].abort();
      wavAbortControllers[side] = null;
    }
    if (side === "a") {
      rawA = "";
      wavsA = [];
      wavsRunA = null;
      if (contentA) { contentA.textContent = ""; contentA.dataset.renderedRun = ""; contentA.dataset.renderedMode = ""; }
      if (headerA) headerA.textContent = "Baseline";
      if (selectA) selectA.value = "";
    } else {
      rawB = "";
      wavsB = [];
      wavsRunB = null;
      if (contentB) { contentB.textContent = ""; contentB.dataset.renderedRun = ""; contentB.dataset.renderedMode = ""; }
      if (headerB) headerB.textContent = "Run to inspect";
      if (selectB) selectB.value = "";
    }
    applyCurrentMode();
  };

  let lastRunsJSON = "";
  const populateSelects = (items) => {
    const selects = [selectA, selectB].filter(Boolean);
    if (selects.length === 0) return;
    const currentJSON = JSON.stringify(items);
    if (currentJSON === lastRunsJSON) return;
    const firstPopulation = !lastRunsJSON;
    lastRunsJSON = currentJSON;

    const prevValues = selects.map(sel => sel.value);
    const runs = [...(items || [])].sort((a, b) => b.id - a.id);
    selects.forEach((sel, selectIndex) => {
      sel.innerHTML = `<option value="">${selectIndex === 0 ? "Select baseline run" : "Select run to inspect"}</option>`;
      runs.forEach((r) => {
        const opt = document.createElement("option");
        opt.value = String(r.id);
        const ts = r.created_at ? new Date(r.created_at).toLocaleString() : "";
        opt.textContent = `#${r.id} · run ${r.run_number} · ${r.status} · ${ts}`;
        if (r.failure_reason) opt.title = `Failure: ${r.failure_reason}`;
        sel.appendChild(opt);
      });
    });

    const validA = runs.some(run => String(run.id) === prevValues[0]);
    const validB = runs.some(run => String(run.id) === prevValues[1]);
    selectA.value = validA ? prevValues[0] : "";
    selectB.value = validB ? prevValues[1] : "";

    // On first use (or after switching testfiles), inspect the newest run and
    // use the run immediately before it as the baseline.
    if (!selectA.value && !selectB.value && runs.length && (firstPopulation || prevValues.some(Boolean))) {
      const current = runs[0];
      const baseline = runs[1];
      if (baseline) selectA.value = String(baseline.id);
      selectB.value = String(current.id);
    }

    if (selectA.value) fetchRun(selectA.value, "a");
    else clearSide("a");
    if (selectB.value) fetchRun(selectB.value, "b");
    else clearSide("b");
  };

  const fetchRun = (id, side) => {
    if (!id) return;
    if (wavAbortControllers[side]) {
      wavAbortControllers[side].abort();
      wavAbortControllers[side] = null;
    }
    const selected = () => String(side === "a" ? selectA.value : selectB.value) === String(id);
    if (side === "a") {
      rawA = "";
      wavsA = [];
      wavsRunA = null;
      if (contentA) { contentA.dataset.renderedRun = ""; contentA.dataset.renderedMode = ""; }
      if (headerA) headerA.textContent = "Baseline · loading…";
    } else {
      rawB = "";
      wavsB = [];
      wavsRunB = null;
      if (contentB) { contentB.dataset.renderedRun = ""; contentB.dataset.renderedMode = ""; }
      if (headerB) headerB.textContent = "Run to inspect · loading…";
    }
    applyCurrentMode();

    fetch(`/api/testrun?id=${id}`)
      .then((r) => r.json())
      .then((j) => {
        if (!selected()) return;
        if (j.status !== "finished") {
          alert(`Error fetching run: ${j.message}`);
          return;
        }

        const label = `#${j.id} · run ${j.run_number} · ${j.result} · ${j.created_at ? new Date(j.created_at).toLocaleString() : ""}`;
        const content = base64DecodeUTF8(j.flow_events_b64);
        if (side === "a") {
          if (headerA) headerA.textContent = `Baseline · ${label}`;
          rawA = content || " ";
        } else {
          if (headerB) headerB.textContent = `Run to inspect · ${label}`;
          rawB = content || " ";
        }
        applyCurrentMode();

        if (activeMode === "wav") {
          fetchWavs(side, id);
        }
      })
      .catch(() => {
        if (!selected()) return;
        if (side === "a" && headerA) headerA.textContent = "Baseline · failed to load";
        if (side === "b" && headerB) headerB.textContent = "Run to inspect · failed to load";
      });
  };

  if (selectA) selectA.onchange = () => selectA.value ? fetchRun(selectA.value, "a") : clearSide("a");
  if (selectB) selectB.onchange = () => selectB.value ? fetchRun(selectB.value, "b") : clearSide("b");

  if (swapBtn) {
    swapBtn.onclick = () => {
      const valueA = selectA?.value || "";
      const valueB = selectB?.value || "";
      if (selectA) selectA.value = valueB;
      if (selectB) selectB.value = valueA;
      if (selectA?.value) fetchRun(selectA.value, "a"); else clearSide("a");
      if (selectB?.value) fetchRun(selectB.value, "b"); else clearSide("b");
    };
  }

  if (clearBtn)
    clearBtn.onclick = () => {
      clearSide("a");
      clearSide("b");
    };

  const switchMode = mode => {
    activeMode = mode;
    if (activeMode === "wav") {
      if (selectA?.value && wavsRunA !== String(selectA.value)) fetchWavs("a", selectA.value);
      if (selectB?.value && wavsRunB !== String(selectB.value)) fetchWavs("b", selectB.value);
    }
    applyCurrentMode();
  };

  if (evtBtn) evtBtn.onclick = () => switchMode("evt");
  if (sipBtn) sipBtn.onclick = () => switchMode("sip");
  if (logBtn) logBtn.onclick = () => switchMode("log");
  if (wavBtn) wavBtn.onclick = () => switchMode("wav");

  if (diffBtn) diffBtn.onclick = () => {
    if (activeMode === "wav") return;
    diffMode = !diffMode;
    applyCurrentMode();
  };

  const getEffectiveTestfileAndProject = () => {
    const info = { name: currentTestfile, project: currentProject };
    if (!info.name && getActiveKey) {
      const key = getActiveKey();
      if (key) {
        const [project, name] = key.split(":");
        return { project, name };
      }
    }
    return info;
  };

  let refreshTimer = null;
  let activeFetchController = null;

  const isCompareVisible = () => {
    return document.getElementById("bottom-row")?.getAttribute("data-bottom-mode") === "compare";
  };

  const refreshList = (force = false) => {
    if (!force && !isCompareVisible()) return;

    if (refreshTimer) {
      clearTimeout(refreshTimer);
    }

    refreshTimer = setTimeout(() => {
      refreshTimer = null;
      const { name, project } = getEffectiveTestfileAndProject();
      if (!name || name === "all") return;

      if (activeFetchController) {
        activeFetchController.abort();
      }
      activeFetchController = new AbortController();

      const url = `/api/testruns?name=${encodeURIComponent(name)}&project=${encodeURIComponent(project)}`;
      fetch(url, { signal: activeFetchController.signal })
        .then((r) => r.json())
        .then((j) => {
          if (j.status === "finished") populateSelects(j.items || []);
        })
        .catch((err) => {
          if (err.name !== "AbortError") {
            // ignore network errors
          }
        })
        .finally(() => {
          activeFetchController = null;
        });
    }, 50);
  };

  if (deleteSelectedBtn) {
    deleteSelectedBtn.onclick = () => {
      const ids = [...new Set([selectA?.value, selectB?.value].filter(Boolean))];
      if (!ids.length || !confirm("Delete selected run(s)? (Running tests will be kept)")) return;
      Promise.all(
        ids.map((id) =>
          fetch(`/api/testrun?id=${id}`, { method: "DELETE" }).then((r) =>
            r.json(),
          ),
        ),
      ).then((results) => {
        results.forEach(
          (j) => j.status !== "finished" && alert(`Error: ${j.message}`),
        );
        refreshList(true);
      });
    };
  }

  if (deleteAllBtn) {
    deleteAllBtn.onclick = () => {
      const { name, project } = getEffectiveTestfileAndProject();
      if (!name || !confirm(`Delete all finished runs for "${name}"? (Running tests will not be deleted)`)) return;
      fetch(
        `/api/testruns?name=${encodeURIComponent(name)}&project=${encodeURIComponent(project)}`,
        { method: "DELETE" },
      )
        .then((r) => r.json())
        .then((j) => {
          if (j.status !== "finished") alert(`Error: ${j.message}`);
          refreshList(true);
        });
    };
  }

  EventBus.on("testfile:changed", (name, project) => {
    const nextProject = project || "";
    if (name !== currentTestfile || nextProject !== currentProject) {
      lastRunsJSON = "";
      clearSide("a");
      clearSide("b");
    }
    currentTestfile = name;
    currentProject = nextProject;
    refreshList();
  });

  EventBus.on("compare:opened", () => {
    const { name, project } = getEffectiveTestfileAndProject();
    currentTestfile = name;
    currentProject = project || "";
    refreshList(true);
  });

  EventBus.on("ws:message", (j) => {
    if (j.token !== "testruns") return;
    if (["save", "start", "list", "delete"].includes(j.action)) {
      const { name, project } = getEffectiveTestfileAndProject();
      const eventFile = j.testfile || j.file;
      const eventProject = j.project;
      if (
        !name ||
        name === "all" ||
        (eventFile === name && (!eventProject || eventProject === project))
      ) {
        refreshList();
      }
      if (j.action === "delete") {
        if (j.id !== undefined) {
          if (String(selectA?.value) === String(j.id)) clearSide("a");
          if (String(selectB?.value) === String(j.id)) clearSide("b");
        } else if (eventFile === name && (!eventProject || eventProject === project)) {
          clearSide("a");
          clearSide("b");
        }
      }
    }
  });

  EventBus.on("log-filter:changed", () => {
    if (activeMode === "log") applyCurrentMode();
  });
  loadLogFilters().catch(err => console.warn("Failed to load log filters for comparison:", err));

  window.addEventListener("resize", schedulePairAlignment);
  applyCurrentMode();
}


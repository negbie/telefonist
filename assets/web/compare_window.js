import { EventBus } from "./event_bus.js";
import {
  escapeHTML,
  base64DecodeUTF8,
  computeLCSDiff,
} from "./utils.js";

export function initCompareWindow(deps) {
  const { getActiveKey } = deps;
  const selectA = document.getElementById("compare-run-a");
  const selectB = document.getElementById("compare-run-b");
  const contentA = document.getElementById("compare-content-a");
  const contentB = document.getElementById("compare-content-b");
  const headerA = document.getElementById("compare-header-a");
  const headerB = document.getElementById("compare-header-b");
  const clearBtn = document.getElementById("compare-clear");
  const deleteSelectedBtn = document.getElementById("compare-delete-selected");
  const diffBtn = document.getElementById("compare-diff");
  const evtBtn = document.getElementById("compare-evt");
  const sipBtn = document.getElementById("compare-sip");
  const logBtn = document.getElementById("compare-log");
  const wavBtn = document.getElementById("compare-wav");
  const deleteAllBtn = document.getElementById("compare-delete-all");

  const comparePanel = document.getElementById("compare-panel");
  if (comparePanel) {
    comparePanel.addEventListener("click", (e) => {
      const seq = e.target.closest(".evt-seq");
      if (!seq) return;

      e.preventDefault();
      e.stopPropagation();

      const idx = seq.getAttribute("data-idx");
      const side = seq.closest("#compare-content-a") ? "a" : "b";
      const targetSide = side === "a" ? "b" : "a";
      const targetContent = document.getElementById(
        `compare-content-${targetSide}`,
      );

      if (targetContent) {
        const target = targetContent.querySelector(
          `.evt-seq[data-idx="${idx}"]`,
        );
        if (target) {
          target.scrollIntoView({ behavior: "smooth", block: "center" });
          // Briefly highlight the target
          const details = target.closest("details");
          if (details) {
            const originalBg = details.style.backgroundColor;
            details.style.backgroundColor = "#fffee0";
            setTimeout(() => (details.style.backgroundColor = originalBg), 1000);
          }
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
  let diffMode = false;
  let activeMode = "evt"; // evt, sip, log, wav

  const renderEventHTML = (j) => {
    const type = (j.type || j.token || "EVENT").toUpperCase();
    const content = Object.entries(j)
      .sort(([a], [b]) => a.localeCompare(b))
      .filter(([k, v]) => v != null && v !== "" && !["type", "token", "class", "event", "time", "id", "RawJSON", "run_id", "idx"].includes(k))
      .map(([k, v]) => {
        const pk = k.charAt(0).toUpperCase() + k.slice(1);
        return `<div class="json-row"><span class="json-key">${escapeHTML(pk)}</span>: <span class="json-value">${escapeHTML(String(v))}</span></div>`;
      })
      .join("");

    return `
<details class="event list" open>
    <summary>
        <span class="evt-seq" data-idx="${j.idx}" title="Sync other side">${j.idx + 1}</span>
        <span class="evt-type">${type}</span>
    </summary>
    <div class="evt-content">${content}</div>
</details>`.trim();
  };

  const structuralSkeleton = (text) => {
    if (!text) return "";
    const rules = [
      [/^\d{2}:\d{2}:\d{2}\.\d{3}[| ]?/, ""],
      [/\b(?:\d{1,3}\.){3}\d{1,3}\b/g, "#"],
      [/([0-9a-fA-F]{1,4}:){2,7}[0-9a-fA-F]{1,4}/g, "#"],
      [/[a-zA-Z0-9]{24,}/g, "#"],
      [/(CSeq|Call-ID|tag|branch|User-Agent|Server|Via):[^ \n\r]+/gi, "$1:#"],
    ];
    let l = text;
    rules.forEach(([reg, rep]) => (l = l.replace(reg, rep)));
    return l.split("\n").map(line => line.trim()).filter(Boolean).join("\n");
  };

  const parseEvents = (text) => {
    return (text || "").split(/\n\n+/).map(s => s.trim()).filter(Boolean).map(line => {
      try { return JSON.parse(line); } catch (e) { return null; }
    }).filter(Boolean).filter(j => {
      const type = (j.type || j.token || "").toLowerCase();
      if (activeMode === "sip") return type === "sip" && !/\d+\s+OPTIONS/i.test(j.param || "");
      if (activeMode === "log") return type === "log";
      return activeMode !== "evt" || !["sip", "log", "testfile"].includes(type);
    }).map((j, idx) => {
      const { time, id, RawJSON, run_id, idx: _, ...rest } = j;
      const compare = ["sip", "log"].includes(activeMode) ? structuralSkeleton(j.param || "") : JSON.stringify({ ...rest, param: rest.param ? structuralSkeleton(rest.param) : undefined }, Object.keys(rest).sort());
      return { display: renderEventHTML({ ...j, idx }), compare, idx };
    });
  };

  const renderRaw = (el, text) => {
    if (el) el.innerHTML = parseEvents(text).map(e => e.display).join("");
  };

  const renderDiff = () => {
    const aItems = parseEvents(rawA), bItems = parseEvents(rawB);
    if (activeMode === "evt") {
      aItems.sort((a, b) => a.compare.localeCompare(b.compare));
      bItems.sort((a, b) => a.compare.localeCompare(b.compare));
    }
    const diff = computeLCSDiff(aItems, bItems);
    if (contentA) contentA.innerHTML = diff.filter(d => d.type === "a").map(d => `<div class="diff-only-a">${d.text}</div>`).join("") || "(no changes)";
    if (contentB) contentB.innerHTML = diff.filter(d => d.type === "b").map(d => `<div class="diff-only-b">${d.text}</div>`).join("") || "(no changes)";
  };

  const applyCurrentMode = () => {
    if (!contentA || !contentB) return;
    if (activeMode === "wav") {
      renderWavs(contentA, wavsA);
      renderWavs(contentB, wavsB);
    } else if (diffMode) {
      renderDiff();
    } else {
      renderRaw(contentA, rawA);
      renderRaw(contentB, rawB);
    }
    [evtBtn, sipBtn, logBtn, wavBtn].forEach(b => b?.classList.toggle("active", b.id === `compare-${activeMode}`));
    if (diffBtn) {
      diffBtn.classList.toggle("active", diffMode);
      diffBtn.textContent = diffMode ? "Raw" : "Diff";
    }
  };

  const renderWavs = (el, wavs) => {
    if (!el) return;
    if (!wavs?.length) return el.innerHTML = "<div class='wav-empty'>No recordings.</div>";
    el.innerHTML = `<div class='wav-list'>${wavs.map(w => `
      <div class='wav-item'>
        <div class='wav-info'>${escapeHTML(w.filename)}</div>
        <canvas id='canvas-wav-${w.id}' class='wav-waveform'></canvas>
        <audio id='audio-wav-${w.id}' controls src='/api/testrun/wav?id=${w.id}'></audio>
      </div>`).join("")}</div>`;
    wavs.forEach(w => drawWaveform(document.getElementById(`canvas-wav-${w.id}`), `/api/testrun/wav?id=${w.id}`, document.getElementById(`audio-wav-${w.id}`)));
  };

  const drawWaveform = async (canvas, url, audioEl) => {
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
      const resp = await fetch(url), buffer = await resp.arrayBuffer();
      const audioBuffer = await (new (window.AudioContext || window.webkitAudioContext)()).decodeAudioData(buffer);
      const rawData = audioBuffer.getChannelData(0);
      draw(rawData);
      audioEl.ontimeupdate = () => draw(rawData, audioEl.currentTime / audioEl.duration);
    } catch (e) {
      ctx.fillStyle = "#ef4444"; ctx.fillText("Error", 10, 20);
    }
  };

  const clearSide = (side) => {
    if (side === "a") {
      rawA = "";
      wavsA = [];
      if (contentA) {
        contentA.textContent = "";
        contentA.removeAttribute("style");
      }
      if (headerA) headerA.textContent = "Run A";
      if (selectA) selectA.value = "";
    } else {
      rawB = "";
      wavsB = [];
      if (contentB) {
        contentB.textContent = "";
        contentB.removeAttribute("style");
      }
      if (headerB) headerB.textContent = "Run B";
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
    lastRunsJSON = currentJSON;

    const prevValues = selects.map((s) => s.value);
    selects.forEach((sel) => {
      sel.innerHTML = '<option value="">(select run)</option>';
      (items || []).forEach((r) => {
        const opt = document.createElement("option");
        opt.value = String(r.id);
        const ts = r.created_at ? new Date(r.created_at).toLocaleString() : "";
        const fileLabel = r.testfile_name ? `${r.testfile_name} / ` : "";
        opt.textContent = `${fileLabel}#${r.id} run ${r.run_number} [${r.status}] ${ts}`;
        sel.appendChild(opt);
      });
    });
    selects.forEach((sel, i) => {
      sel.value = prevValues[i] || "";
    });
  };

  const fetchRun = (id, side) => {
    if (!id) return;
    fetch(`/api/testrun?id=${id}`)
      .then((r) => r.json())
      .then((j) => {
        if (j.status === "finished") {
          const label = `#${j.id} run ${j.run_number} [${j.result}] ${j.created_at ? new Date(j.created_at).toLocaleString() : ""}`;
          const content = base64DecodeUTF8(j.flow_events_b64) || "(decode error)";

          if (side === "a") {
            headerA && (headerA.textContent = `Run A – ${label}`);
            rawA = content;
          } else {
            headerB && (headerB.textContent = `Run B – ${label}`);
            rawB = content;
          }

          fetch(`/api/testrun/wavs?id=${id}`)
            .then((r) => r.json())
            .then((wj) => {
              if (wj.status === "finished") {
                const items = wj.items || [];
                items.sort((a, b) => {
                  const tsA = (a.filename.match(
                    /\d{4}-\d{2}-\d{2}-\d{2}-\d{2}-\d{2}/,
                  ) || [""])[0];
                  const tsB = (b.filename.match(
                    /\d{4}-\d{2}-\d{2}-\d{2}-\d{2}-\d{2}/,
                  ) || [""])[0];
                  return (
                    tsA.localeCompare(tsB) || a.filename.localeCompare(b.filename)
                  );
                });
                if (side === "a") wavsA = items;
                else wavsB = items;
                applyCurrentMode();
              }
            });
        } else {
          alert(`Error fetching run: ${j.message}`);
        }
      });
  };

  if (selectA) selectA.onchange = () => fetchRun(selectA.value, "a");
  if (selectB) selectB.onchange = () => fetchRun(selectB.value, "b");

  if (clearBtn)
    clearBtn.onclick = () => {
      clearSide("a");
      clearSide("b");
    };

  const scrolls = {};
  const getScrollKey = (m, d) => `${m}${d ? "_diff" : ""}`;

  const switchMode = (mode) => {
    const key = getScrollKey(activeMode, diffMode);
    if (!scrolls[key]) scrolls[key] = { a: 0, b: 0 };
    scrolls[key].a = contentA ? contentA.scrollTop : 0;
    scrolls[key].b = contentB ? contentB.scrollTop : 0;

    if (mode) activeMode = mode;
    applyCurrentMode();

    const nextKey = getScrollKey(activeMode, diffMode);
    if (scrolls[nextKey]) {
      if (contentA) contentA.scrollTop = scrolls[nextKey].a;
      if (contentB) contentB.scrollTop = scrolls[nextKey].b;
    }
  };

  if (evtBtn) evtBtn.onclick = () => switchMode("evt");
  if (sipBtn) sipBtn.onclick = () => switchMode("sip");
  if (logBtn) logBtn.onclick = () => switchMode("log");
  if (wavBtn)
    wavBtn.onclick = () => {
      activeMode = "wav";
      diffMode = false;
      applyCurrentMode();
    };

  if (diffBtn)
    diffBtn.onclick = () => {
      if (activeMode === "wav") return;
      const key = getScrollKey(activeMode, diffMode);
      if (!scrolls[key]) scrolls[key] = { a: 0, b: 0 };
      scrolls[key].a = contentA ? contentA.scrollTop : 0;
      scrolls[key].b = contentB ? contentB.scrollTop : 0;

      diffMode = !diffMode;
      applyCurrentMode();

      const nextKey = getScrollKey(activeMode, diffMode);
      if (scrolls[nextKey]) {
        if (contentA) contentA.scrollTop = scrolls[nextKey].a;
        if (contentB) contentB.scrollTop = scrolls[nextKey].b;
      }
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

  const refreshList = () => {
    const { name, project } = getEffectiveTestfileAndProject();
    let url = "/api/testruns";
    if (name && name !== "all") {
      url += `?name=${encodeURIComponent(name)}&project=${encodeURIComponent(project)}`;
    }
    fetch(url)
      .then((r) => r.json())
      .then((j) => {
        if (j.status === "finished") populateSelects(j.items || []);
      });
  };

  if (deleteSelectedBtn) {
    deleteSelectedBtn.onclick = () => {
      const ids = [...new Set([selectA?.value, selectB?.value].filter(Boolean))];
      if (!ids.length || !confirm("Delete selected run(s)?")) return;
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
        refreshList();
        fetch("/api/maintenance", { method: "POST" });
      });
    };
  }

  if (deleteAllBtn) {
    deleteAllBtn.onclick = () => {
      const { name, project } = getEffectiveTestfileAndProject();
      if (!name || !confirm(`Delete ALL runs for "${name}"?`)) return;
      fetch(
        `/api/testruns?name=${encodeURIComponent(name)}&project=${encodeURIComponent(project)}`,
        { method: "DELETE" },
      )
        .then((r) => r.json())
        .then((j) => {
          if (j.status !== "finished") alert(`Error: ${j.message}`);
          refreshList();
          fetch("/api/maintenance", { method: "POST" });
        });
    };
  }

  EventBus.on("testfile:changed", (name, project) => {
    currentTestfile = name;
    currentProject = project || "";
    refreshList();
  });

  EventBus.on("ws:open", refreshList);

  EventBus.on("ws:message", (j) => {
    if (j.token !== "testruns") return;
    if (["save", "list", "delete"].includes(j.action)) {
      const { name, project } = getEffectiveTestfileAndProject();
      if (
        !name ||
        name === "all" ||
        (j.testfile === name && j.project === project)
      ) {
        refreshList();
      }
      if (j.action === "delete") {
        if (j.id !== undefined) {
          if (String(selectA?.value) === String(j.id)) clearSide("a");
          if (String(selectB?.value) === String(j.id)) clearSide("b");
        } else if (j.testfile === name && j.project === project) {
          clearSide("a");
          clearSide("b");
        }
      }
    }
  });
}


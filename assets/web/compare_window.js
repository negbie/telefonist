window.initCompareWindow = (deps) => {
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

    // Use safeText from utils.js or define as fallback
    const escapeHTML = window.escapeHTML || ((t) => {
        if (!t) return "";
        return String(t).replace(/[&<>"']/g, m => ({
            '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
        })[m]);
    });

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
            const targetContent = document.getElementById(`compare-content-${targetSide}`);

            if (targetContent) {
                const target = targetContent.querySelector(`.evt-seq[data-idx="${idx}"]`);
                if (target) {
                    target.scrollIntoView({ behavior: "smooth", block: "center" });
                    // Briefly highlight the target
                    const details = target.closest("details");
                    if (details) {
                        const originalBg = details.style.backgroundColor;
                        details.style.backgroundColor = "#fffee0";
                        setTimeout(() => details.style.backgroundColor = originalBg, 1000);
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
        const eventType = (j.type || j.token || "EVENT").toUpperCase();
        const time = j.time || "??:??:??";
        let peer = escapeHTML(j.peeruri || j.contacturi || "");

        let summaryLine = peer;
        if (eventType === "SIP") {
            const lines = (j.param || "").split("\n");
            const dir = (lines[0] || "").includes("|RX") ? "RX" : "TX";
            const methodLine = (lines[2] || "").trim();
            summaryLine = `<span class="sip-dir">${dir}</span> ${escapeHTML(methodLine)}`;
        } else if (eventType === "LOG") {
            summaryLine = escapeHTML((j.param || "").substring(0, 100));
        } else if (!peer) {
            summaryLine = escapeHTML(j.param || "").substring(0, 50);
        }

        const prettyPrintEvent = (event) => {
            let res = "";
            Object.keys(event).sort().forEach(k => {
                if (["type", "token", "class", "event", "time", "id", "RawJSON", "run_id", "idx"].includes(k)) return;
                const val = event[k];
                if (val !== undefined && val !== null && val !== "") {
                    const pk = k.charAt(0).toUpperCase() + k.slice(1);
                    res += `<div class="json-row"><span class="json-key">${escapeHTML(pk)}</span>: <span class="json-value">${escapeHTML(String(val))}</span></div>`;
                }
            });
            return res;
        };

        const content = prettyPrintEvent(j);

        return `
<details class="event list" open>
    <summary>
        <span class="evt-seq" data-idx="${j.idx}" title="Sync other side">${j.idx + 1}</span>
        <span class="evt-type">${eventType}</span>
    </summary>
    <div class="evt-content">
        ${content}
    </div>
</details>`.trim();
    };

    const structuralSkeleton = (text) => {
        if (!text) return "";
        return text.split("\n").map(function (line) {
            // Strip timestamp: HH:MM:SS.mmm| or HH:MM:SS.mmm followed by space/nothing
            var cleaned = line.replace(/^\d{2}:\d{2}:\d{2}\.\d{3}[| ]?/, "");
            var trimmed = cleaned.trim();
            if (!trimmed || trimmed.toLowerCase().startsWith("a=crypto")) return "";

            var words = trimmed.split(/\s+/);
            return words.map(function (word) {
                // Split by semicolon OR comma to handle complex SIP parameters (Via, tags, RTCP, etc.)
                return word.split(/[;,]/).map(function (sub) {
                    // Normalize IP addresses (v4/v6)
                    if (/\b(?:\d{1,3}\.){3}\d{1,3}\b/.test(sub) || sub.includes("::")) {
                        if (sub.indexOf("=") !== -1) return sub.split("=")[0] + "=#";
                        return "#";
                    }

                    var isVariable = /\d/.test(sub) || /[a-zA-Z0-9]{12,}/.test(sub) || /[\/\+\-\*\?\!\&]/.test(sub);
                    if (isVariable) {
                        if (sub.indexOf("=") !== -1) return sub.split("=")[0] + "=#";
                        if (sub.indexOf(":") !== -1) {
                            var parts = sub.split(":");
                            if (parts[0].length < 10 && !/\d/.test(parts[0])) return parts[0] + ":#";
                            return "#";
                        }
                        return "#";
                    }
                    return sub;
                }).join(";");
            }).join(" ");
        }).filter(Boolean).join("\n");
    };

    const parseEvents = (text) => {
        const all = (text || "").split(/\n\n+/).map(s => s.trim()).filter(Boolean).map(line => {
            try { return JSON.parse(line); } catch (e) { return null; }
        }).filter(Boolean);

        const filtered = all.filter(j => {
            const type = (j.type || j.token || "EVENT").toLowerCase();
            if (activeMode === "sip") return type === "sip";
            if (activeMode === "log") return type === "log";
            if (activeMode === "evt") {
                return j.type !== "SIP" && j.type !== "LOG" && j.token !== "testfile";
            }
            return true;
        });

        return filtered.map(function (j, idx) {
            j.idx = idx;
            var display = renderEventHTML(j);

            var compare;
            if (activeMode === "sip" || activeMode === "log") {
                compare = structuralSkeleton(j.param || "");
            } else {
                // For regular events, use precise JSON-based matching but skeletonize the param
                var comp = JSON.parse(JSON.stringify(j)); // Deep clone
                ["time", "id", "RawJSON", "run_id", "idx"].forEach(function (k) { delete comp[k]; });
                if (comp.param) comp.param = structuralSkeleton(comp.param);
                compare = JSON.stringify(comp, Object.keys(comp).sort());
            }

            return { display: display, compare: compare, idx: idx };
        });
    };

    const renderRaw = (el, text) => {
        if (!el) return;
        const filtered = parseEvents(text);
        el.innerHTML = filtered.map(e => e.display).join("");
    };

    const renderDiff = () => {
        if (!rawA && !rawB) return;

        let aItems = parseEvents(rawA);
        let bItems = parseEvents(rawB);

        // Sorting is important for events that might arrive out of order,
        // but for SIP and LOG, the chronological order is crucial for flow analysis.
        //if (activeMode !== "sip" && activeMode !== "log") {
        aItems.sort((a, b) => a.compare.localeCompare(b.compare));
        bItems.sort((a, b) => a.compare.localeCompare(b.compare));
        //}

        const diff = computeLCSDiff(aItems, bItems);
        const hasDiffs = diff.some(d => d.type !== "common");

        if (!hasDiffs && aItems.length > 0) {
            const msg = "<div class='diff-identical'>No differences found. Runs are identical.</div>";
            if (contentA) contentA.innerHTML = msg;
            if (contentB) contentB.innerHTML = msg;
            return;
        }

        let htmlA = "", htmlB = "";
        diff.forEach(d => {
            if (d.type === "common") return;
            if (d.type === "a") {
                htmlA += `<div class="diff-only-a">${d.text}</div>`;
            } else if (d.type === "b") {
                htmlB += `<div class="diff-only-b">${d.text}</div>`;
            }
        });

        if (contentA) contentA.innerHTML = htmlA || "(empty)";
        if (contentB) contentB.innerHTML = htmlB || "(empty)";
    };

    const applyCurrentMode = () => {
        if (!contentA || !contentB) return;
        contentA.style.whiteSpace = "normal";
        contentB.style.whiteSpace = "normal";

        contentA.style.whiteSpace = "normal";
        contentB.style.whiteSpace = "normal";

        if (activeMode === "wav") {
            renderWavs(contentA, wavsA);
            renderWavs(contentB, wavsB);
        } else if (diffMode) {
            renderDiff();
        } else {
            renderRaw(contentA, rawA);
            renderRaw(contentB, rawB);
        }

        evtBtn?.classList.toggle("active", activeMode === "evt");
        sipBtn?.classList.toggle("active", activeMode === "sip");
        logBtn?.classList.toggle("active", activeMode === "log");
        wavBtn?.classList.toggle("active", activeMode === "wav");
        diffBtn?.classList.toggle("active", diffMode);

        if (diffBtn) diffBtn.textContent = diffMode ? "Raw" : "Diff";
    };

    const renderWavs = (containerEl, wavs) => {
        if (!containerEl) return;
        if (!wavs || wavs.length === 0) {
            containerEl.innerHTML = "<div class='wav-empty'>No recordings found for this run.</div>";
            return;
        }

        containerEl.innerHTML = `<div class='wav-list'>${wavs.map(w => `
            <div class='wav-item'>
                <div class='wav-info' title='${escapeHTML(w.filename)}'>${escapeHTML(w.filename)}</div>
                <div class='wav-waveform'><canvas id='canvas-wav-${w.id}'></canvas></div>
                <audio id='audio-wav-${w.id}' controls src='/api/testrun/wav?id=${w.id}'></audio>
            </div>`).join('')}</div>`;

        wavs.forEach(w => {
            const canvas = document.getElementById(`canvas-wav-${w.id}`);
            const audio = document.getElementById(`audio-wav-${w.id}`);
            if (canvas && audio) drawWaveform(canvas, `/api/testrun/wav?id=${w.id}`, audio);
        });
    };

    const drawWaveform = (canvas, url, audioEl) => {
        const ctx = canvas.getContext("2d");
        const width = canvas.offsetWidth || 400;
        const height = canvas.offsetHeight || 120;
        canvas.width = width;
        canvas.height = height;

        let waveformData = null;

        const render = (currentTime) => {
            if (!waveformData) return;
            ctx.clearRect(0, 0, width, height);
            ctx.fillStyle = "#ffffff";
            ctx.fillRect(0, 0, width, height);

            const multiplier = height / (Math.max(...waveformData) || 1);
            ctx.fillStyle = "#3b82f6";
            waveformData.forEach((val, i) => {
                const h = val * multiplier;
                ctx.fillRect(i, (height - h) / 2, 1, h);
            });

            if (currentTime !== undefined && audioEl.duration) {
                const x = (currentTime / audioEl.duration) * width;
                ctx.fillStyle = "#ef4444";
                ctx.fillRect(x, 0, 2, height);
            }
        };

        ctx.fillStyle = "#f3f4f6";
        ctx.fillRect(0, 0, width, height);
        ctx.fillStyle = "#3b82f6";
        ctx.font = "10px sans-serif";
        ctx.fillText("Loading waveform...", 10, 20);

        fetch(url)
            .then(r => r.arrayBuffer())
            .then(ab => (new (window.AudioContext || window.webkitAudioContext)()).decodeAudioData(ab))
            .then(audioBuffer => {
                const rawData = audioBuffer.getChannelData(0);
                const blockSize = Math.floor(rawData.length / width);
                waveformData = [];
                for (let i = 0; i < width; i++) {
                    const blockStart = blockSize * i;
                    let sum = 0;
                    for (let j = 0; j < blockSize; j++) sum += Math.abs(rawData[blockStart + j]);
                    waveformData.push(sum / blockSize);
                }
                render();
                audioEl.ontimeupdate = () => render(audioEl.currentTime);
            })
            .catch(err => {
                console.error("Error drawing waveform:", err);
                ctx.fillStyle = "#ef4444";
                ctx.fillText("Error loading waveform", 10, 20);
            });
    };

    const clearSide = (side) => {
        if (side === "a") {
            rawA = ""; wavsA = [];
            if (contentA) { contentA.textContent = ""; contentA.removeAttribute("style"); }
            if (headerA) headerA.textContent = "Run A";
            if (selectA) selectA.value = "";
        } else {
            rawB = ""; wavsB = [];
            if (contentB) { contentB.textContent = ""; contentB.removeAttribute("style"); }
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

        const prevValues = selects.map(s => s.value);
        selects.forEach(sel => {
            sel.innerHTML = '<option value="">(select run)</option>';
            (items || []).forEach(r => {
                const opt = document.createElement("option");
                opt.value = String(r.id);
                const ts = r.created_at ? new Date(r.created_at).toLocaleString() : "";
                const fileLabel = r.testfile_name ? `${r.testfile_name} / ` : "";
                opt.textContent = `${fileLabel}#${r.id} run ${r.run_number} [${r.status}] ${ts}`;
                sel.appendChild(opt);
            });
        });
        selects.forEach((sel, i) => { sel.value = prevValues[i] || ""; });
    };

    const fetchRun = (id, side) => {
        if (!id) return;
        fetch(`/api/testrun?id=${id}`).then(r => r.json()).then(j => {
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

                fetch(`/api/testrun/wavs?id=${id}`).then(r => r.json()).then(wj => {
                    if (wj.status === "finished") {
                        if (side === "a") wavsA = wj.items || [];
                        else wavsB = wj.items || [];
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

    if (clearBtn) clearBtn.onclick = () => { clearSide("a"); clearSide("b"); };

    const scrolls = { evt: { a: 0, b: 0 }, sip: { a: 0, b: 0 }, log: { a: 0, b: 0 } };
    const switchMode = (mode) => {
        if (!diffMode && scrolls[activeMode]) {
            scrolls[activeMode].a = contentA ? contentA.scrollTop : 0;
            scrolls[activeMode].b = contentB ? contentB.scrollTop : 0;
        }
        if (mode) activeMode = mode;
        applyCurrentMode();
        if (!diffMode && scrolls[activeMode]) {
            if (contentA) contentA.scrollTop = scrolls[activeMode].a;
            if (contentB) contentB.scrollTop = scrolls[activeMode].b;
        }
    };

    if (evtBtn) evtBtn.onclick = () => switchMode("evt");
    if (sipBtn) sipBtn.onclick = () => switchMode("sip");
    if (logBtn) logBtn.onclick = () => switchMode("log");
    if (wavBtn) wavBtn.onclick = () => { activeMode = "wav"; diffMode = false; applyCurrentMode(); };

    if (diffBtn) diffBtn.onclick = () => {
        if (activeMode === "wav") return;
        if (!diffMode && scrolls[activeMode]) {
            scrolls[activeMode].a = contentA ? contentA.scrollTop : 0;
            scrolls[activeMode].b = contentB ? contentB.scrollTop : 0;
        }
        diffMode = !diffMode;
        applyCurrentMode();
        if (!diffMode && scrolls[activeMode]) {
            if (contentA) contentA.scrollTop = scrolls[activeMode].a;
            if (contentB) contentB.scrollTop = scrolls[activeMode].b;
        }
    };

    const getEffectiveTestfileAndProject = () => {
        const info = { name: currentTestfile, project: currentProject };
        if (!info.name && window._tfManager?.getActiveKey) {
            const key = window._tfManager.getActiveKey();
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
        fetch(url).then(r => r.json()).then(j => {
            if (j.status === "finished") populateSelects(j.items || []);
        });
    };

    if (deleteSelectedBtn) {
        deleteSelectedBtn.onclick = () => {
            const ids = [...new Set([selectA?.value, selectB?.value].filter(Boolean))];
            if (!ids.length || !confirm("Delete selected run(s)?")) return;
            Promise.all(ids.map(id => fetch(`/api/testrun?id=${id}`, { method: "DELETE" }).then(r => r.json())))
                .then(results => {
                    results.forEach(j => j.status !== "finished" && alert(`Error: ${j.message}`));
                    refreshList();
                    fetch("/api/maintenance", { method: "POST" });
                });
        };
    }

    if (deleteAllBtn) {
        deleteAllBtn.onclick = () => {
            const { name, project } = getEffectiveTestfileAndProject();
            if (!name || !confirm(`Delete ALL runs for "${name}"?`)) return;
            fetch(`/api/testruns?name=${encodeURIComponent(name)}&project=${encodeURIComponent(project)}`, { method: "DELETE" })
                .then(r => r.json()).then(j => {
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
            if (!name || name === "all" || (j.testfile === name && j.project === project)) {
                refreshList();
            }
            if (j.action === "delete") {
                if (j.id !== undefined) {
                    if (String(selectA?.value) === String(j.id)) clearSide("a");
                    if (String(selectB?.value) === String(j.id)) clearSide("b");
                } else if (j.testfile === name && j.project === project) {
                    clearSide("a"); clearSide("b");
                }
            }
        }
    });
};

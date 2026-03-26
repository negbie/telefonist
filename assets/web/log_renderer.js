const createDownloadButton = (label, runId, type) => {
    const btn = document.createElement("button");
    btn.textContent = label;
    btn.className = "btn-download";
    btn.onclick = (e) => {
        e.stopPropagation();
        const link = document.createElement("a");
        link.href = `/api/testrun/download?id=${runId}&type=${type}`;
        link.download = "";
        link.style.display = "none";
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
    };
    return btn;
};

window.renderLogEvent = (j, elements, getOptions) => {
    const { logViewEl, sipViewEl, flowEl, searchLogInput } = elements;

    const addSep = (container, type) => {
        if (!container) return;
        const isStart = type === "start";
        const runId = isStart ? j._runId : (j.run_id || window._lastRunId || `fin_${Date.now()}`);
        const name = j.file || j.name || (j.token === "test" ? "Interactive Run" : "Test Run");

        const sep = document.createElement("div");
        sep.className = "run-separator " + (isStart ? "start" : (j.result === "FAIL" ? "end-fail" : "end-pass"));

        let text = (j.token === "testfile" ? "─── " : "─── Run ") + (isStart ? "Testfile " : (type === "finished" ? "Finished: " : "Started: ")) + safeText(name);
        if (type === "finished" && j.result) {
            text += " [" + j.result + "]";
        }
        sep.textContent = text + " ───";
        sep.title = "Click to sync scroll";
        if (isStart) sep.dataset.runStartId = runId;
        else sep.dataset.runFinishId = runId;

        sep.onclick = () => {
            const attr = isStart ? "data-run-start-id" : "data-run-finish-id";
            document.querySelectorAll(`div[${attr}="${runId}"]`).forEach(el => el.scrollIntoView({ behavior: 'smooth', block: 'center' }));
        };

        container.appendChild(sep);

        if (!isStart && j.run_id) {
            const bc = document.createElement("div");
            bc.className = "download-container";
            if (container === flowEl) bc.appendChild(createDownloadButton("Download Flow (.txt)", j.run_id, "flow"));
            else if (container === sipViewEl) {
                bc.appendChild(createDownloadButton("Download SIP (.txt)", j.run_id, "sip"));
                bc.appendChild(createDownloadButton("Download PCAP", j.run_id, "pcap"));
            } else if (container === logViewEl) bc.appendChild(createDownloadButton("Download Log (.txt)", j.run_id, "log"));
            container.appendChild(bc);
        }

        trimChildrenToMax(container, getOptions().maxItems);
        if (getOptions().autoscroll) container.scrollTop = container.scrollHeight;
    };

    if ((j.token === "testfile" || j.token === "test") && j.status === "running") {
        j._runId = `run_${Date.now()}_${Math.floor(Math.random() * 1000)}`;
        window._lastRunId = j._runId;
        [logViewEl, sipViewEl, flowEl].forEach(c => addSep(c, "start"));
        return false;
    }

    if ((j.token === "testfile" || j.token === "test") && j.status === "finished") {
        if (j.run_id && document.querySelector(`div[data-run-finish-id="${j.run_id}"]`)) return true;
        [logViewEl, sipViewEl, flowEl].forEach(c => addSep(c, "finished"));
        return false;
    }

    if (j.type === "LOG" && logViewEl) {
        const el = document.createElement("div");
        el.className = "log-row";
        el.style.fontFamily = "var(--font-mono)";
        el.style.fontSize = "12px";
        el.style.whiteSpace = "pre-wrap";
        el.textContent = j.param;

        if (searchLogInput?.value) {
            const filter = searchLogInput.value.toLowerCase();
            if (!el.textContent.toLowerCase().includes(filter)) el.style.display = "none";
        }

        logViewEl.appendChild(el);
        trimChildrenToMax(logViewEl, getOptions().maxItems);
        if (getOptions().autoscroll) logViewEl.scrollTop = logViewEl.scrollHeight;
        return true;
    }

    if (j.type === "CMD") {
        const renderCmd = (container) => {
            if (!container) return;
            const el = document.createElement("div");
            el.className = "run-separator cmd";
            if (j._cmdId) {
                el.dataset.cmdId = j._cmdId;
                el.style.cursor = "pointer";
                el.onclick = () => {
                    document.querySelectorAll(`div[data-cmd-id="${j._cmdId}"]`).forEach(e => e.scrollIntoView({ behavior: 'smooth', block: 'center' }));
                };
            }
            el.textContent = "─── " + safeText(j.param) + " ───";
            container.appendChild(el);
            trimChildrenToMax(container, getOptions().maxItems);
            if (getOptions().autoscroll) container.scrollTop = container.scrollHeight;
        };
        [logViewEl, sipViewEl, flowEl].forEach(renderCmd);
        return true;
    }

    return false;
};

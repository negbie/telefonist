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
  const { logViewEl, sipViewEl, flowEl } = elements;

  const opts = getOptions();

  const appendAndMaintain = (container, el) => {
    if (!container || !el) return;
    container.appendChild(el);
    trimChildrenToMax(container, opts.maxItems);
    if (opts.autoscroll) container.scrollTop = container.scrollHeight;
  };

  const runLabel = (isStart, type, token, name, result, project, hash, runId) => {
    const prefix = token === "testfile" ? "─── " : "─── Run ";
    const phase = isStart
      ? "Testfile "
      : type === "finished"
        ? "Finished: "
        : "Started: ";
    const suffix = type === "finished" && result ? ` [${result}]` : "";
    let label = `${prefix}${phase}${safeText(name)}${suffix} ───`;
    if (type === "finished" && token === "testfile") {
      if (project) label += ` Project: ${safeText(project)}`;
      if (hash) label += `, Hash: ${safeText(hash)}`;
      if (runId) label += `, Run: ${safeText(runId)}`;
    }
    return label;
  };

  const addSep = (container, type) => {
    if (!container) return;
    const isStart = type === "start";
    const runId = isStart
      ? j._runId
      : j.run_id || window._lastRunId || `fin_${Date.now()}`;
    const name =
      j.file || j.name || (j.token === "test" ? "Interactive Run" : "Test Run");

    const sep = document.createElement("div");
    sep.className =
      "run-separator " +
      (isStart ? "start" : j.result === "FAIL" ? "end-fail" : "end-pass");
    sep.setAttribute("data-token", j.token || "test");

    sep.textContent = runLabel(
      isStart,
      type,
      j.token,
      name,
      j.result,
      j.project,
      j.actual_hash,
      runId,
    );
    sep.title = "Click to sync scroll";
    if (isStart) sep.dataset.runStartId = runId;
    else sep.dataset.runFinishId = runId;

    sep.onclick = () => {
      const attr = isStart ? "data-run-start-id" : "data-run-finish-id";
      requestAnimationFrame(() => {
        document
          .querySelectorAll(`div[${attr}="${runId}"]`)
          .forEach((el) =>
            el.scrollIntoView({ behavior: "smooth", block: "center" }),
          );
      });
    };

    appendAndMaintain(container, sep);

    if (!isStart && j.run_id) {
      const bc = document.createElement("div");
      bc.className = "download-container";
      if (container === flowEl)
        bc.appendChild(
          createDownloadButton("Download Flow (.txt)", j.run_id, "flow"),
        );
      else if (container === sipViewEl) {
        bc.appendChild(
          createDownloadButton("Download SIP (.txt)", j.run_id, "sip"),
        );
        bc.appendChild(createDownloadButton("Download PCAP", j.run_id, "pcap"));
      } else if (container === logViewEl)
        bc.appendChild(
          createDownloadButton("Download Log (.txt)", j.run_id, "log"),
        );
      appendAndMaintain(container, bc);
    }
  };

  if (
    (j.token === "testfile" || j.token === "test") &&
    j.status === "running"
  ) {
    j._runId = `run_${Date.now()}_${Math.floor(Math.random() * 1000)}`;
    window._lastRunId = j._runId;
    [logViewEl, sipViewEl, flowEl].forEach((c) => addSep(c, "start"));
    return false;
  }

  if (
    (j.token === "testfile" || j.token === "test") &&
    j.status === "finished"
  ) {
    if (
      j.run_id &&
      document.querySelector(`div[data-run-finish-id="${j.run_id}"]`)
    )
      return true;
    [logViewEl, sipViewEl, flowEl].forEach((c) => addSep(c, "finished"));
    return true;
  }

  if (j.type === "LOG" && logViewEl) {
    const el = document.createElement("div");
    el.className = "log-row";
    el.textContent = j.param;

    appendAndMaintain(logViewEl, el);
    return true;
  }

  if (j.type === "CMD") {
    const renderCmd = (container) => {
      if (!container) return;
      const el = document.createElement("div");
      el.className = "run-separator cmd";
      el.setAttribute("data-token", j.token || "test");
      // Do not set data-has-result="1" as CMD events are not results themselves.
      if (j._cmdId) {
        el.dataset.cmdId = j._cmdId;
        el.style.cursor = "pointer";
        el.onclick = () => {
          requestAnimationFrame(() => {
            document
              .querySelectorAll(`div[data-cmd-id="${j._cmdId}"]`)
              .forEach((e) =>
                e.scrollIntoView({ behavior: "smooth", block: "center" }),
              );
          });
        };
      }
      el.textContent = "─── " + safeText(j.param) + " ───";
      appendAndMaintain(container, el);
    };
    [logViewEl, sipViewEl, flowEl].forEach(renderCmd);
    return true;
  }

  return false;
};

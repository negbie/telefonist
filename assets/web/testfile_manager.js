import { API } from "./api.js";
import { EventBus } from "./event_bus.js";
import { createStateManager } from "./state_manager.js";
import {
  safeText,
  base64EncodeUTF8,
  base64DecodeUTF8,
  sanitizeTestfileName,
} from "./utils.js";
import { syntaxHighlight } from "./highlighter.js";

export function initTestfileManager(deps) {
  const {
    socket,
    testfileInputEl,
    testfilesRunEl,
    testfilesStopEl,
    testfileSelectEl,
    testfilesNewEl,
    testfilesSaveEl,
    testfilesRenameEl,
    testfilesCloneEl,
    testfilesDeleteEl,
    testfileHighlightsEl,
    onActiveFileChange,
    renderError,
  } = deps;

  const projectSelectEl = document.getElementById("project-select");
  const projectsNewEl = document.getElementById("projects-new");
  const projectsRenameEl = document.getElementById("projects-rename");
  const projectsCloneEl = document.getElementById("projects-clone");
  const projectsDeleteEl = document.getElementById("projects-delete");

  const state = createStateManager();
  window.addEventListener("beforeunload", event => {
    if (state.getKeys().some(state.isDirty)) { event.preventDefault(); event.returnValue = ""; }
  });
  const api = API;

  let activeKey = "";
  let savedTestfiles = [];
  let isTestRunning = false;

  const wsIsOpen = () => socket && socket.isOpen();

  function getSelectedKeys() {
    if (!testfileSelectEl) return [];
    return Array.from(testfileSelectEl.selectedOptions)
      .filter((opt) => opt.value)
      .map((opt) => opt.value);
  }

  function setActiveKey(key) {
    activeKey = key || "";
    syncSelectToActive();
    updateEnabledStates();
    if (typeof onActiveFileChange === "function") {
      onActiveFileChange(activeKey);
    }
  }

  function updateEnabledStates() {
    const isDirty = state.isDirty(activeKey);
    if (testfilesSaveEl)
      testfilesSaveEl.disabled = !wsIsOpen() || !activeKey || !isDirty;

    const keys = getSelectedKeys();
    if (testfilesRenameEl)
      testfilesRenameEl.disabled =
        !wsIsOpen() || keys.length !== 1 || isTestRunning;
    if (testfilesCloneEl)
      testfilesCloneEl.disabled =
        !wsIsOpen() || keys.length !== 1 || isTestRunning;
    if (testfilesDeleteEl)
      testfilesDeleteEl.disabled =
        !wsIsOpen() || keys.length === 0 || isTestRunning;
    if (testfilesRunEl)
      testfilesRunEl.disabled = !wsIsOpen() || isTestRunning;
    if (testfilesStopEl)
      testfilesStopEl.disabled = !wsIsOpen() || !isTestRunning;
  }

  function syncSelectToActive() {
    if (!testfileSelectEl) return;
    populateTestfileSelect(savedTestfiles);
    testfileSelectEl.value = activeKey || "";
  }

  function requestLists(overrides = {}) {
    updateEnabledStates();
    const p1 = api.getProjects().then((j) => {
      if (j.status === "finished")
        populateProjectsSelect(j.items, overrides.project);
    });
    const p2 = api.getTestfiles().then((j) => {
      if (j.status === "finished") populateTestfileSelect(j.items);
    });
    return Promise.all([p1, p2]).catch((err) => {
      console.warn("Failed to load testfiles or projects:", err);
    });
  }

  // --- Actions ---
  function reportError(error) {
    alert("Request failed: " + error.message);
  }

  async function promptAction({ prompt, initial = "", sanitize = false }, fn) {
    let name = window.prompt(prompt, initial);
    if (!name) return;
    name = sanitize ? sanitizeTestfileName(name) : safeText(name).trim();
    if (!name) return;
    try {
      const res = await fn(name);
      if (res && res.status !== "finished") alert("Error: " + res.message);
      else await requestLists();
    } catch (err) {
      reportError(err);
    }
  }

  function handleNew() {
    const project = projectSelectEl?.value;
    if (!project) return alert("Please select a project first.");
    promptAction({ prompt: "Enter name for new testfile:", sanitize: true }, async (name) => {
      const key = `${project}:${name}`;
      state.updateCache(key, { name, project, original: "", current: "" });
      setActiveKey(key);
      if (testfileInputEl) { testfileInputEl.value = ""; updateHighlights(); }
      return api.saveTestfile(name, project, base64EncodeUTF8(""));
    });
  }

  async function handleSave(forceRevision = null) {
    if (typeof forceRevision !== "string") {
      forceRevision = null;
    }
    const savedKey = activeKey;
    const entry = activeKey && state.getEntry(activeKey);
    if (!entry || !testfileInputEl) return;
    const content = safeText(testfileInputEl.value || "");
    try {
      let revision = forceRevision || entry.revision;
      if (revision === undefined) {
        const stored = await api.getTestfile(entry.name, entry.project);
        if (stored.status !== "finished" || base64DecodeUTF8(stored.content_b64).replace(/\r/g, "") !== entry.original.replace(/\r/g, ""))
          throw new Error("The server version differs from this draft; reload and merge before saving");
        revision = stored.updated_at;
      }
      const j = await api.saveTestfile(entry.name, entry.project, base64EncodeUTF8(content), revision);
      if (j.status === "finished") {
        state.updateCache(savedKey, { original: content, revision: j.revision });
        updateEnabledStates();
        updateCurrentOptionLabel();
      } else alert("Error: " + j.message);
    } catch (error) {
      const msg = error && error.message ? error.message : "";
      if (msg.includes("file changed on server") || msg.includes("The server version differs")) {
        if (confirm("Conflict: The file changed on the server.\n\nDo you want to OVERWRITE the server version with your current edits?")) {
          try {
            const j = await api.saveTestfile(entry.name, entry.project, base64EncodeUTF8(content), null);
            if (j.status === "finished") {
              state.updateCache(savedKey, { original: content, revision: j.revision });
              updateEnabledStates();
              updateCurrentOptionLabel();
              return;
            } else {
              alert("Error: " + j.message);
              return;
            }
          } catch (fetchErr) {
            reportError(fetchErr);
            return;
          }
        } else if (confirm("Do you want to DISCARD your local edits and reload the server version?")) {
          try {
            const stored = await api.getTestfile(entry.name, entry.project);
            if (stored && stored.content_b64) {
              const serverContent = base64DecodeUTF8(stored.content_b64).replace(/\r/g, "");
              state.updateCache(savedKey, { original: serverContent, current: serverContent, revision: stored.updated_at });
              testfileInputEl.value = serverContent;
              updateHighlights();
              updateEnabledStates();
              updateCurrentOptionLabel();
            }
          } catch (fetchErr) {
            reportError(fetchErr);
          }
          return;
        }
      }
      reportError(error);
    }
  }

  function handleRename() {
    const key = getSelectedKeys()[0], entry = key && state.getEntry(key);
    if (!entry) return;
    promptAction({ prompt: `New name for "${entry.name}":`, initial: entry.name, sanitize: true }, async (newName) => {
      if (newName === entry.name) return;
      const j = await api.renameTestfile(entry.name, entry.project, newName, entry.project);
      if (j.status === "finished") {
        const newKey = `${entry.project}:${newName}`;
        state.updateCache(newKey, { ...entry, name: newName });
        state.deleteEntry(key);
        setActiveKey(newKey);
      }
      return j;
    });
  }

  function handleClone() {
    const key = getSelectedKeys()[0], entry = key && state.getEntry(key);
    if (!entry) return;
    promptAction({ prompt: `Clone "${entry.name}" as:`, initial: entry.name + "_copy", sanitize: true }, async (newName) => {
      const j = await api.cloneTestfile(entry.project, entry.name, newName);
      if (j.status === "finished") {
        const newKey = `${entry.project}:${newName}`;
        state.updateCache(newKey, { ...entry, name: newName, original: entry.original, current: entry.current });
        setActiveKey(newKey);
      }
      return j;
    });
  }

  async function handleDelete() {
    const keys = getSelectedKeys();
    if (!keys.length || !confirm(`Delete ${keys.length} selected testfile(s)?`)) return;
    try {
      await Promise.all(keys.map(k => {
        const e = state.getEntry(k);
        return e ? api.deleteTestfile(e.name, e.project).then(j => { if (j.status === "finished") state.deleteEntry(k); }) : null;
      }));
      setActiveKey("");
      if (testfileInputEl) { testfileInputEl.value = ""; updateHighlights(); }
      await requestLists();
    } catch (err) { reportError(err); }
  }

  function handleRun() {
    if (!wsIsOpen()) return;
    const keys = getSelectedKeys();
    if (keys.length === 0) {
      return alert("Please select a testfile first.");
    }
    if (keys.length > 1) {
      const args = keys.map(k => { const e = state.getEntry(k); return e ? `${e.project || "''"} ${e.name}` : ""; }).filter(Boolean).join(" ");
      socket.send(`testfiles run ${args}`);
    } else {
      const key = keys[0] || activeKey, entry = state.getEntry(key);
      const content = testfileInputEl ? safeText(testfileInputEl.value || "").trim() : "";
      if (content) socket.send(entry ? `testfile_inline ${entry.project || "''"} ${entry.name} ${base64EncodeUTF8(content)}` : `testfile_inline ${base64EncodeUTF8(content)}`);
      else if (entry) socket.send(`testfiles run ${entry.project || "''"} ${entry.name}`);
    }
  }

  function handleStop() { wsIsOpen() && socket.send("test_stop"); }

  const handleNewProject = () =>
    promptAction({ prompt: "New project name:" }, name => api.createProject(name).then(() => requestLists({ project: name })));

  const handleRenameProject = () => {
    const old = projectSelectEl?.value;
    if (!old) return alert("Please select a project first.");
    promptAction({ prompt: `Rename "${old}" to:`, initial: old }, name => api.renameProject(old, name).then(() => requestLists({ project: name })));
  };

  const handleCloneProject = () => {
    const src = projectSelectEl?.value;
    if (!src) return alert("Please select a project first.");
    promptAction({ prompt: `Clone "${src}" as:`, initial: src + "_copy" }, name => api.cloneProject(src, name).then(() => requestLists({ project: name })));
  };

  const handleDeleteProject = async () => {
    const name = projectSelectEl?.value;
    if (!name) return alert("Please select a project first.");
    if (confirm(`Delete project "${name}"?`)) {
      try {
        await api.deleteProject(name);
        await requestLists();
      } catch (err) { reportError(err); }
    }
  };

  // --- DOM Listeners ---

  if (testfilesNewEl) testfilesNewEl.onclick = handleNew;
  if (testfilesSaveEl) testfilesSaveEl.onclick = () => handleSave();
  if (testfilesRenameEl) testfilesRenameEl.onclick = handleRename;
  if (testfilesCloneEl) testfilesCloneEl.onclick = handleClone;
  if (testfilesDeleteEl) testfilesDeleteEl.onclick = handleDelete;
  if (testfilesRunEl) testfilesRunEl.onclick = handleRun;
  if (testfilesStopEl) testfilesStopEl.onclick = handleStop;

  if (projectsNewEl) projectsNewEl.onclick = handleNewProject;
  if (projectsRenameEl) projectsRenameEl.onclick = handleRenameProject;
  if (projectsCloneEl) projectsCloneEl.onclick = handleCloneProject;
  if (projectsDeleteEl) projectsDeleteEl.onclick = handleDeleteProject;

  if (projectSelectEl) projectSelectEl.onchange = () => populateTestfileSelect(savedTestfiles);

  if (testfileSelectEl) {
    let lastOptgroupToggleTime = 0;
    const handleOptgroupClick = (target) => {
      if (!target || target.tagName !== "OPTGROUP") return;
      const now = Date.now();
      if (now - lastOptgroupToggleTime < 200) return;
      lastOptgroupToggleTime = now;

      const options = Array.from(target.querySelectorAll("option"));
      if (options.length === 0) return;
      const allSelected = options.every((opt) => opt.selected);
      options.forEach((opt) => {
        opt.selected = !allSelected;
      });

      updateEnabledStates();
      if (typeof testfileSelectEl.onchange === "function") {
        testfileSelectEl.onchange();
      }
    };

    testfileSelectEl.addEventListener("mousedown", (e) => {
      if (e.target && e.target.tagName === "OPTGROUP") {
        e.preventDefault();
        handleOptgroupClick(e.target);
      }
    });

    testfileSelectEl.addEventListener("touchstart", (e) => {
      if (e.target && e.target.tagName === "OPTGROUP") {
        e.preventDefault();
        handleOptgroupClick(e.target);
      }
    }, { passive: false });

    testfileSelectEl.onchange = () => {
      const keys = getSelectedKeys(); updateEnabledStates();
      if (keys.length !== 1 || keys[0] === activeKey) return;
      const key = keys[0]; let entry = state.getEntry(key);
      if (!entry) { const [p, n] = key.split(":"); state.updateCache(key, { project: p, name: n }); entry = state.getEntry(key); }
      if (testfileInputEl) { testfileInputEl.value = entry.current || ""; updateHighlights(); }
      api.getTestfile(entry.name, entry.project).then(j => {
        if (j.status === "finished") {
          const content = base64DecodeUTF8(j.content_b64).replace(/\r/g, ""), wasDirty = state.isDirty(key);
          if (!wasDirty) state.updateCache(key, { original: content, current: content, revision: j.updated_at });
          if (key === activeKey && testfileInputEl && !wasDirty) { testfileInputEl.value = content; updateHighlights(); }
          syncSelectToActive(); updateEnabledStates();
        }
      });
      setActiveKey(key);
    };
  }

  if (testfileInputEl) {
    testfileInputEl.oninput = () => {
      if (!activeKey) return;
      state.updateCache(activeKey, { current: testfileInputEl.value });
      updateEnabledStates(); updateHighlights(); updateCurrentOptionLabel();
    };
    testfileInputEl.onscroll = () => {
      if (testfileHighlightsEl) {
        testfileHighlightsEl.scrollTop = testfileInputEl.scrollTop;
        testfileHighlightsEl.scrollLeft = testfileInputEl.scrollLeft;
      }
    };
  }

  function updateHighlights() { if (typeof syntaxHighlight === "function") syntaxHighlight(testfileInputEl, testfileHighlightsEl); }

  let measureCtx = null;
  function adjustTestfileSelectWidth() {
    if (!testfileSelectEl || typeof document === "undefined") return;
    const options = Array.from(testfileSelectEl.options || []);
    const optgroups = Array.from(testfileSelectEl.getElementsByTagName("optgroup"));

    let fontSize = "13px";
    let fontFamily = 'system-ui, -apple-system, "Segoe UI", Roboto, Arial, sans-serif';
    if (typeof window !== "undefined" && window.getComputedStyle) {
      try {
        const computed = window.getComputedStyle(testfileSelectEl);
        if (computed.fontSize) fontSize = computed.fontSize;
        if (computed.fontFamily) fontFamily = computed.fontFamily;
      } catch (_) { }
    }

    if (!measureCtx && typeof document.createElement === "function") {
      try {
        const canvas = document.createElement("canvas");
        measureCtx = canvas.getContext ? canvas.getContext("2d") : null;
      } catch (_) { }
    }

    let maxTextWidth = 0;
    if (measureCtx) {
      measureCtx.font = `${fontSize} ${fontFamily}`;
      for (const opt of options) {
        const text = opt.textContent || opt.label || "";
        const w = measureCtx.measureText(text).width;
        if (w > maxTextWidth) maxTextWidth = w;
      }
      for (const og of optgroups) {
        const text = og.label || "";
        const w = measureCtx.measureText(text).width;
        if (w > maxTextWidth) maxTextWidth = w;
      }
    } else {
      for (const opt of options) {
        const len = (opt.textContent || opt.label || "").length;
        if (len * 8 > maxTextWidth) maxTextWidth = len * 8;
      }
      for (const og of optgroups) {
        const len = (og.label || "").length;
        if (len * 8 > maxTextWidth) maxTextWidth = len * 8;
      }
    }

    // Overhead: optgroup indent & option padding (~26px) + vertical scrollbar (~20px) + border/buffer (~18px) = 64px
    const requiredSelectWidth = Math.max(240, Math.ceil(maxTextWidth + 64));
    const expandedWidth = 80 + 64 + requiredSelectWidth;

    const testControls = document.getElementById("test-controls");
    if (testControls?.style) {
      testControls.style.setProperty("--test-controls-width", `${expandedWidth}px`);
    }
  }

  function updateCurrentOptionLabel() {
    const opt = activeKey && Array.from(testfileSelectEl?.options || []).find(o => o.value === activeKey);
    if (opt) {
      let label = state.isDirty(activeKey) ? `${state.getEntry(activeKey).name} [unsaved]` : state.getEntry(activeKey).name;
      opt.textContent = label;
      adjustTestfileSelectWidth();
    }
  }

  function populateProjectsSelect(projects, forcedValue) {
    if (!projectSelectEl) return;
    const current = forcedValue ?? projectSelectEl.value;
    projectSelectEl.innerHTML = '<option value="">(all projects)</option>';
    (projects || []).forEach(p => projectSelectEl.appendChild(new Option(p.name, p.name)));
    projectSelectEl.value = Array.from(projectSelectEl.options).some(o => o.value === current) ? current : "";
    if (forcedValue !== undefined) populateTestfileSelect(savedTestfiles);
  }

  function populateTestfileSelect(items) {
    if (!testfileSelectEl) return;
    savedTestfiles = items || [];
    const filter = projectSelectEl?.value || "", serverKeys = savedTestfiles.map(it => `${it.project}:${it.name}`);
    savedTestfiles.forEach(it => state.updateCache(`${it.project}:${it.name}`, { name: it.name, project: it.project }));
    state.pruneStale(serverKeys);

    testfileSelectEl.innerHTML = "";
    const groups = {};
    state.getKeys().forEach(k => {
      const e = state.getEntry(k);
      if (!filter || e.project === filter) (groups[e.project || ""] = groups[e.project || ""] || []).push(e);
    });

    if (filter && !groups[filter]) {
      groups[filter] = [];
    }

    Object.keys(groups).sort().forEach(p => {
      const group = document.createElement("optgroup");
      group.label = p || "Uncategorized";
      testfileSelectEl.appendChild(group);
      groups[p].sort((a, b) => a.name.localeCompare(b.name)).forEach(e => {
        const k = `${e.project}:${e.name}`;
        let label = state.isDirty(k) ? e.name + " [unsaved]" : e.name;
        const opt = new Option(label, k);
        opt.title = `${p || "Uncategorized"} / ${e.name}`;
        opt.selected = (k === activeKey);
        group.appendChild(opt);
      });
    });

    if (activeKey && filter && state.getEntry(activeKey)?.project !== filter) {
      setActiveKey(""); if (testfileInputEl) { testfileInputEl.value = ""; updateHighlights(); }
    }
    adjustTestfileSelectWidth();
    updateEnabledStates();
  }

  if (typeof document !== "undefined" && document.fonts?.ready) {
    document.fonts.ready.then(adjustTestfileSelectWidth).catch(() => { });
  }
  adjustTestfileSelectWidth();

  // Fetch testfiles and projects immediately over HTTP on module load:
  requestLists();

  return {
    updateSaveEnabled: updateEnabledStates,
    requestTestfilesList: requestLists,
    getActiveKey: () => activeKey,
    handleTestfilesMessage: (j) => {
      if (j.token === "runner") {
        isTestRunning = !!j.state?.active;
        updateEnabledStates();
        return true;
      }
      const isTF =
        j.token === "testfile" || j.token === "testfiles" || j.token === "test";
      if (!isTF && j.token !== "projects") return false;

      updateEnabledStates();

      if (
        j.action === "save" ||
        j.action === "delete" ||
        j.action === "rename" ||
        j.token === "projects"
      ) {
        requestLists();
        if (j.token === "testfiles" && j.name)
          EventBus.emit("testfile:changed", j.name, j.project);
        return true;
      }

      if (j.status === "finished" && j.content_b64 && testfileInputEl) {
        const content = base64DecodeUTF8(j.content_b64).replace(/\r/g, "");
        const key = `${j.project || ""}:${j.name || ""}`;
        const wasDirty = state.isDirty(key);
        state.updateCache(key, {
          original: content,
          current: wasDirty ? undefined : content,
        });

        if (key === activeKey) {
          testfileInputEl.value = state.getEntry(key).current;
          updateHighlights();
        }
        syncSelectToActive();
      }
      return isTF;
    },
  };
}


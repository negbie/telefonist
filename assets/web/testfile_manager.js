/* testfile_manager.js */
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
    if (!wsIsOpen()) return Promise.resolve();
    const p1 = api.getProjects().then((j) => {
      if (j.status === "finished")
        populateProjectsSelect(j.items, overrides.project);
    });
    const p2 = api.getTestfiles().then((j) => {
      if (j.status === "finished") populateTestfileSelect(j.items);
    });
    return Promise.all([p1, p2]);
  }

  // --- Actions ---

  const runAction = (promptMsg, apiCall, refresh = true, overrides = {}) => {
    let name = window.prompt(promptMsg.msg, promptMsg.initial);
    if (!name || (promptMsg.sanitize && !(name = sanitizeTestfileName(name)))) return;
    apiCall(name).then((j) => {
      if (j.status !== "finished") alert("Error: " + j.message);
      else if (refresh) requestLists(overrides);
    });
  };

  function handleNew() {
    runAction({ msg: "Enter name for new testfile:", sanitize: true }, (name) => {
      const project = projectSelectEl?.value || "", key = `${project}:${name}`;
      state.updateCache(key, { name, project, original: "", current: "" });
      setActiveKey(key);
      if (testfileInputEl) { testfileInputEl.value = ""; updateHighlights(); }
      return api.saveTestfile(name, project, base64EncodeUTF8(""));
    });
  }

  function handleSave() {
    const entry = activeKey && state.getEntry(activeKey);
    if (!entry || !testfileInputEl) return;
    const content = safeText(testfileInputEl.value || "").trim();
    api.saveTestfile(entry.name, entry.project, base64EncodeUTF8(content)).then((j) => {
      if (j.status === "finished") {
        state.updateCache(activeKey, { original: content });
        updateEnabledStates();
        updateCurrentOptionLabel();
      } else alert("Error: " + j.message);
    });
  }

  function handleRename() {
    const key = getSelectedKeys()[0], entry = key && state.getEntry(key);
    if (!entry) return;
    runAction({ msg: `New name for "${entry.name}":`, initial: entry.name, sanitize: true }, (newName) =>
      api.renameTestfile(entry.name, entry.project, newName, entry.project).then(j => {
        if (j.status === "finished") {
          const newKey = `${entry.project}:${newName}`;
          state.updateCache(newKey, { ...entry, name: newName });
          state.deleteEntry(key);
          setActiveKey(newKey);
        }
        return j;
      })
    );
  }

  function handleDelete() {
    const keys = getSelectedKeys();
    if (!keys.length || !confirm(`Delete ${keys.length} selected testfile(s)?`)) return;
    Promise.all(keys.map(k => api.deleteTestfile(state.getEntry(k).name, state.getEntry(k).project).then(j => {
      if (j.status === "finished") state.deleteEntry(k);
      return j;
    }))).then(() => { setActiveKey(""); if (testfileInputEl) testfileInputEl.value = ""; requestLists(); });
  }

  function handleRun() {
    if (!wsIsOpen()) return;
    const keys = getSelectedKeys();
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

  // --- DOM Listeners ---

  if (testfilesNewEl) testfilesNewEl.onclick = handleNew;
  if (testfilesSaveEl) testfilesSaveEl.onclick = handleSave;
  if (testfilesRenameEl) testfilesRenameEl.onclick = handleRename;
  if (testfilesDeleteEl) testfilesDeleteEl.onclick = handleDelete;
  if (testfilesRunEl) testfilesRunEl.onclick = handleRun;
  if (testfilesStopEl) testfilesStopEl.onclick = handleStop;

  if (projectsNewEl) projectsNewEl.onclick = () => runAction({ msg: "New project name:" }, (name) => api.createProject(name), true, { project: name });
  if (projectsRenameEl) projectsRenameEl.onclick = () => {
    const old = projectSelectEl?.value;
    if (!old) return alert("Select project first.");
    runAction({ msg: `Rename "${old}" to:`, initial: old }, (name) => api.renameProject(old, name), true, { project: name });
  };
  if (projectsCloneEl) projectsCloneEl.onclick = () => {
    const src = projectSelectEl?.value;
    if (!src) return alert("Select project first.");
    runAction({ msg: `Clone "${src}" as:`, initial: src + "_copy" }, (name) => api.cloneProject(src, name), true, { project: name });
  };
  if (projectsDeleteEl) projectsDeleteEl.onclick = () => {
    const name = projectSelectEl?.value;
    if (name && confirm(`Delete project "${name}"?`)) api.deleteProject(name).then(requestLists);
  };

  if (projectSelectEl) projectSelectEl.onchange = () => populateTestfileSelect(savedTestfiles);

  if (testfileSelectEl) {
    testfileSelectEl.onchange = () => {
      const keys = getSelectedKeys(); updateEnabledStates();
      if (keys.length !== 1 || keys[0] === activeKey) return;
      const key = keys[0]; let entry = state.getEntry(key);
      if (!entry) { const [p, n] = key.split(":"); state.updateCache(key, { project: p, name: n }); entry = state.getEntry(key); }
      if (testfileInputEl) { testfileInputEl.value = entry.current || ""; updateHighlights(); }
      api.getTestfile(entry.name, entry.project).then(j => {
        if (j.status === "finished") {
          const content = base64DecodeUTF8(j.content_b64).replace(/\r/g, ""), wasDirty = state.isDirty(key);
          state.updateCache(key, { original: content, current: wasDirty ? undefined : content });
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

  function updateCurrentOptionLabel() {
    const opt = activeKey && Array.from(testfileSelectEl?.options || []).find(o => o.value === activeKey);
    if (opt) {
      let label = state.isDirty(activeKey) ? `${state.getEntry(activeKey).name} [unsaved]` : state.getEntry(activeKey).name;
      opt.textContent = label.length > 30 ? label.substring(0, 30) + "..." : label;
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

    Object.keys(groups).sort().forEach(p => {
      let container = testfileSelectEl;
      if (Object.keys(groups).length > 1) {
        const group = document.createElement("optgroup"); group.label = p || "Uncategorized";
        testfileSelectEl.appendChild(group); container = group;
      }
      groups[p].sort((a,b) => a.name.localeCompare(b.name)).forEach(e => {
        const k = `${e.project}:${e.name}`;
        let label = state.isDirty(k) ? e.name + " [unsaved]" : e.name;
        const opt = new Option(label.length > 30 ? label.substring(0, 30) + "..." : label, k);
        opt.selected = (k === activeKey); container.appendChild(opt);
      });
    });

    if (activeKey && filter && state.getEntry(activeKey)?.project !== filter) {
      setActiveKey(""); if (testfileInputEl) { testfileInputEl.value = ""; updateHighlights(); }
    }
    updateEnabledStates();
  }

  return {
    updateSaveEnabled: updateEnabledStates,
    requestTestfilesList: requestLists,
    getActiveKey: () => activeKey,
    handleTestfilesMessage: (j) => {
      const isTF =
        j.token === "testfile" || j.token === "testfiles" || j.token === "test";
      if (!isTF && j.token !== "projects") return false;

      if (j.token === "testfile" || j.token === "test") {
        if (j.status === "running" || j.status === "progress")
          isTestRunning = true;
        else if (["finished", "stopped", "error"].indexOf(j.status) !== -1)
          isTestRunning = false;
      }
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


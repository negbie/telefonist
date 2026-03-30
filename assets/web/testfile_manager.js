/* testfile_manager.js */
window.initTestfileManager = function (deps) {
    const { socket, testfileInputEl, testfilesRunEl, testfilesStopEl, testfileSelectEl,
        testfilesNewEl, testfilesSaveEl, testfilesRenameEl,
        testfilesDeleteEl, testfileHighlightsEl, onActiveFileChange, renderError } = deps;

    const projectSelectEl = document.getElementById("project-select");
    const projectsNewEl = document.getElementById("projects-new");
    const projectsRenameEl = document.getElementById("projects-rename");
    const projectsCloneEl = document.getElementById("projects-clone");
    const projectsDeleteEl = document.getElementById("projects-delete");

    const state = window.StateManager();
    const api = window.API;

    let activeKey = "";
    let savedTestfiles = [];
    let isTestRunning = false;

    const wsIsOpen = () => socket && socket.isOpen();

    function getSelectedKeys() {
        if (!testfileSelectEl) return [];
        return Array.from(testfileSelectEl.selectedOptions)
            .filter(opt => opt.value)
            .map(opt => opt.value);
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
        if (testfilesSaveEl) testfilesSaveEl.disabled = !wsIsOpen() || !activeKey || !isDirty;

        const keys = getSelectedKeys();
        if (testfilesRenameEl) testfilesRenameEl.disabled = !wsIsOpen() || keys.length !== 1 || isTestRunning;
        if (testfilesDeleteEl) testfilesDeleteEl.disabled = !wsIsOpen() || keys.length === 0 || isTestRunning;
        if (testfilesRunEl) testfilesRunEl.disabled = !wsIsOpen() || isTestRunning;
        if (testfilesStopEl) testfilesStopEl.disabled = !wsIsOpen() || !isTestRunning;
    }

    function syncSelectToActive() {
        if (!testfileSelectEl) return;
        populateTestfileSelect(savedTestfiles);
        testfileSelectEl.value = activeKey || "";
    }

    function requestLists(overrides = {}) {
        isTestRunning = false;
        updateEnabledStates();
        if (!wsIsOpen()) return Promise.resolve();
        const p1 = api.getProjects().then(j => {
            if (j.status === "finished") populateProjectsSelect(j.items, overrides.project);
        });
        const p2 = api.getTestfiles().then(j => {
            if (j.status === "finished") populateTestfileSelect(j.items);
        });
        return Promise.all([p1, p2]);
    }

    // --- Actions ---

    function handleNew() {
        let name = window.prompt("Enter name for new testfile:");
        if (!name || !(name = sanitizeTestfileName(name))) return;

        const project = projectSelectEl ? projectSelectEl.value : "";
        const key = `${project}:${name}`;

        state.updateCache(key, { name, project, original: "", current: "" });
        setActiveKey(key);

        if (testfileInputEl) {
            testfileInputEl.value = "";
            updateHighlights();
        }

        api.saveTestfile(name, project, base64EncodeUTF8("")).then(j => {
            if (j.status !== "finished") alert("Error creating testfile: " + j.message);
            else requestLists();
        });
    }

    function handleSave() {
        if (!activeKey || !testfileInputEl) return;
        const entry = state.getEntry(activeKey);
        if (!entry) return;

        const content = safeText(testfileInputEl.value || "").trim();
        api.saveTestfile(entry.name, entry.project, base64EncodeUTF8(content)).then(j => {
            if (j.status === "finished") {
                state.updateCache(activeKey, { original: content });
                updateEnabledStates();
                updateCurrentOptionLabel();
            } else {
                alert("Error saving testfile: " + j.message);
            }
        });
    }

    function handleRename() {
        const keys = getSelectedKeys();
        if (keys.length !== 1) return;
        const oldKey = keys[0];
        const entry = state.getEntry(oldKey);
        if (!entry) return;

        let newName = window.prompt(`New name for "${entry.name}":`, entry.name);
        if (!newName || newName === entry.name || !(newName = sanitizeTestfileName(newName))) return;

        api.renameTestfile(entry.name, entry.project, newName, entry.project).then(j => {
            if (j.status === "finished") {
                const newKey = `${entry.project}:${newName}`;
                state.updateCache(newKey, { ...entry, name: newName });
                state.deleteEntry(oldKey);
                setActiveKey(newKey);
                requestLists();
            } else {
                alert("Error renaming: " + j.message);
            }
        });
    }

    function handleDelete() {
        const keys = getSelectedKeys();
        if (keys.length === 0) return;

        const msg = keys.length === 1
            ? `Delete saved testfile "${state.getEntry(keys[0])?.name || keys[0]}"? This cannot be undone.`
            : `Delete ${keys.length} selected testfiles? This cannot be undone.`;

        if (!window.confirm(msg)) return;

        Promise.all(keys.map(k => {
            const entry = state.getEntry(k);
            return entry ? api.deleteTestfile(entry.name, entry.project).then(j => {
                if (j.status === "finished") {
                    state.deleteEntry(k);
                } else {
                    alert("Error deleting testfile: " + j.message);
                }
            }) : Promise.resolve();
        })).then(() => {
            setActiveKey("");
            if (testfileInputEl) testfileInputEl.value = "";
            requestLists();
        });
    }

    function handleRun() {
        if (!wsIsOpen()) return;
        const keys = getSelectedKeys();

        if (keys.length > 1) {
            const args = keys.map(k => {
                const e = state.getEntry(k);
                return e ? `${e.project || "''"} ${e.name}` : "";
            }).filter(Boolean).join(" ");
            socket.send(`testfiles run ${args}`);
            return;
        }

        const key = keys[0] || activeKey;
        const entry = state.getEntry(key);
        const content = testfileInputEl ? safeText(testfileInputEl.value || "").trim() : "";

        if (content) {
            const b64 = base64EncodeUTF8(content);
            socket.send(entry ? `testfile_inline ${entry.project || "''"} ${entry.name} ${b64}` : `testfile_inline ${b64}`);
        } else if (entry) {
            socket.send(`testfiles run ${entry.project || "''"} ${entry.name}`);
        }
    }

    function handleStop() {
        if (!wsIsOpen()) return;
        socket.send("test_stop");
    }

    // --- DOM Listeners ---

    if (testfilesNewEl) testfilesNewEl.onclick = handleNew;
    if (testfilesSaveEl) testfilesSaveEl.onclick = handleSave;
    if (testfilesRenameEl) testfilesRenameEl.onclick = handleRename;
    if (testfilesDeleteEl) testfilesDeleteEl.onclick = handleDelete;
    if (testfilesRunEl) testfilesRunEl.onclick = handleRun;
    if (testfilesStopEl) testfilesStopEl.onclick = handleStop;

    if (projectsNewEl) projectsNewEl.onclick = () => {
        const name = window.prompt("Enter name for new project:");
        const trimmed = name?.trim();
        if (trimmed) {
            api.createProject(trimmed).then(j => {
                if (j.status !== "finished") alert("Error creating project: " + j.message);
                else requestLists({ project: trimmed });
            });
        }
    };
    if (projectsRenameEl) projectsRenameEl.onclick = () => {
        const oldName = projectSelectEl?.value;
        if (!oldName) {
            alert("Please select a project to rename first.");
            return;
        }
        const newName = window.prompt(`Enter new name for project "${oldName}":`, oldName);
        const trimmed = newName?.trim();
        if (trimmed && trimmed !== oldName) {
            api.renameProject(oldName, trimmed).then(j => {
                if (j.status !== "finished") alert("Error renaming project: " + j.message);
                else requestLists({ project: trimmed });
            }).catch(e => alert("Error renaming project: " + e.message));
        }
    };
    if (projectsCloneEl) projectsCloneEl.onclick = () => {
        const srcName = projectSelectEl?.value;
        if (!srcName) {
            alert("Please select a project to clone first.");
            return;
        }
        const targetName = window.prompt(`Enter name for new cloned project (copy of "${srcName}"):`, srcName + "_copy");
        const trimmed = targetName?.trim();
        if (trimmed && trimmed !== srcName) {
            api.cloneProject(srcName, trimmed).then(j => {
                if (j.status !== "finished") alert("Error cloning project: " + j.message);
                else requestLists({ project: trimmed });
            }).catch(e => alert("Error cloning project: " + e.message));
        }
    };
    if (projectsDeleteEl) projectsDeleteEl.onclick = () => {
        const name = projectSelectEl?.value;
        if (name && window.confirm(`Delete project "${name}"? Test files will be left intact but moved to uncategorized.`)) {
            api.deleteProject(name).then(j => {
                if (j.status !== "finished") alert("Error deleting project: " + j.message);
                else requestLists();
            });
        }
    };

    if (projectSelectEl) projectSelectEl.onchange = () => populateTestfileSelect(savedTestfiles);

    if (testfileSelectEl) {
        testfileSelectEl.onchange = () => {
            const keys = getSelectedKeys();
            updateEnabledStates();
            if (keys.length !== 1 || keys[0] === activeKey) return;

            const key = keys[0];
            let entry = state.getEntry(key);
            if (!entry) {
                const [p, n] = key.split(":");
                state.updateCache(key, { project: p, name: n });
                entry = state.getEntry(key);
            }

            if (testfileInputEl) {
                testfileInputEl.value = entry.current || "";
                updateHighlights();
            }

            api.getTestfile(entry.name, entry.project).then(j => {
                if (j.status === "finished") {
                    const content = base64DecodeUTF8(j.content_b64).replace(/\r/g, "");
                    const wasDirty = state.isDirty(key);
                    state.updateCache(key, { original: content, current: wasDirty ? undefined : content });
                    if (key === activeKey && testfileInputEl && !wasDirty) {
                        testfileInputEl.value = content;
                        updateHighlights();
                    }
                    syncSelectToActive();
                    updateEnabledStates();
                }
            });
            setActiveKey(key);
        };
    }

    if (testfileInputEl) {
        testfileInputEl.oninput = () => {
            if (activeKey) {
                state.updateCache(activeKey, { current: testfileInputEl.value });
                updateEnabledStates();
                updateHighlights();
                updateCurrentOptionLabel();
            }
        };
        testfileInputEl.onscroll = () => {
            if (testfileHighlightsEl) {
                testfileHighlightsEl.scrollTop = testfileInputEl.scrollTop;
                testfileHighlightsEl.scrollLeft = testfileInputEl.scrollLeft;
            }
        };
    }

    // --- Helpers ---

    function updateHighlights() {
        if (typeof syntaxHighlight === "function") syntaxHighlight(testfileInputEl, testfileHighlightsEl);
    }

    function updateCurrentOptionLabel() {
        if (!testfileSelectEl || !activeKey) return;
        const opt = Array.from(testfileSelectEl.options).find(o => o.value === activeKey);
        if (opt) {
            let label = state.isDirty(activeKey) ? `${state.getEntry(activeKey).name} [unsaved]` : state.getEntry(activeKey).name;
            if (label.length > 30) label = label.substring(0, 30) + '...';
            opt.textContent = label;
        }
    }

    function populateProjectsSelect(projects, forcedValue) {
        if (!projectSelectEl) return;
        const current = forcedValue !== undefined ? forcedValue : projectSelectEl.value;
        projectSelectEl.innerHTML = '<option value="">(all projects)</option>';
        (projects || []).forEach(p => {
            const opt = new Option(p.name, p.name);
            projectSelectEl.appendChild(opt);
        });
        projectSelectEl.value = Array.from(projectSelectEl.options).some(o => o.value === current) ? current : "";
        if (forcedValue !== undefined) {
            // If we forced a project selection, we must update the testfile list too.
            populateTestfileSelect(savedTestfiles);
        }
    }

    let lastTestfilesJSON = "";
    function populateTestfileSelect(items) {
        if (!testfileSelectEl) return;
        savedTestfiles = items || [];

        const currentJSON = JSON.stringify(savedTestfiles) + (projectSelectEl?.value || "");
        if (currentJSON === lastTestfilesJSON) return;
        lastTestfilesJSON = currentJSON;

        const filter = projectSelectEl?.value || "";
        const serverKeys = savedTestfiles.map(it => `${it.project}:${it.name}`);
        
        savedTestfiles.forEach(it => state.updateCache(`${it.project}:${it.name}`, { name: it.name, project: it.project }));
        state.pruneStale(serverKeys);

        testfileSelectEl.innerHTML = "";
        const groups = {};
        state.getKeys().forEach(key => {
            const entry = state.getEntry(key);
            if (!filter || entry.project === filter) {
                (groups[entry.project] = groups[entry.project] || []).push(entry);
            }
        });

        const sorted = Object.keys(groups).sort();
        if (sorted.includes("")) sorted.push(sorted.splice(sorted.indexOf(""), 1)[0]);

        sorted.forEach(p => {
            let container = testfileSelectEl;
            if (sorted.length > 1) {
                const group = document.createElement("optgroup");
                group.label = p || "Uncategorized";
                testfileSelectEl.appendChild(group);
                container = group;
            }
            groups[p].sort((a,b) => a.name.localeCompare(b.name)).forEach(e => {
                const k = `${e.project}:${e.name}`;
                let label = state.isDirty(k) ? e.name + " [unsaved]" : e.name;
                if (label.length > 30) label = label.substring(0, 30) + '...';
                const opt = new Option(label, k);
                opt.selected = (k === activeKey);
                container.appendChild(opt);
            });
        });
        
        // If the active key is not in the filtered list, clear the input
        if (activeKey && filter && state.getEntry(activeKey)?.project !== filter) {
            setActiveKey("");
            if (testfileInputEl) {
                testfileInputEl.value = "";
                updateHighlights();
            }
        }
        
        updateEnabledStates();
    }

    return {
        updateSaveEnabled: updateEnabledStates,
        requestTestfilesList: requestLists,
        getActiveKey: () => activeKey,
        handleTestfilesMessage: (j) => {
            const isTF = j.token === "testfile" || j.token === "testfiles";
            if (!isTF && j.token !== "projects") return false;

            if (j.status === "running" || j.status === "progress") isTestRunning = true;
            else if (["finished", "stopped", "error"].indexOf(j.status) !== -1) isTestRunning = false;
            updateEnabledStates();

            if (j.action === "save" || j.action === "delete" || j.action === "rename" || j.token === "projects") {
                requestLists();
                if (j.token === "testfiles" && j.name) EventBus.emit("testfile:changed", j.name, j.project);
                return true;
            }

            if (j.status === "finished" && j.content_b64 && testfileInputEl) {
                const content = base64DecodeUTF8(j.content_b64).replace(/\r/g, "");
                const key = `${j.project || ""}:${j.name || ""}`;
                const wasDirty = state.isDirty(key);
                state.updateCache(key, { original: content, current: wasDirty ? undefined : content });

                if (key === activeKey) {
                    testfileInputEl.value = state.getEntry(key).current;
                    updateHighlights();
                }
                syncSelectToActive();
            }
            return isTF;
        }
    };
};

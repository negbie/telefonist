import { escapeHTML, initModal } from "./utils.js";
import { EventBus } from "./event_bus.js";
import { API } from "./api.js";

export function initCronManager() {
    const refreshBtn = document.getElementById("cron-refresh");
    const newBtn = document.getElementById("cron-new");
    const listEl = document.getElementById("cron-list");

    const modal = document.getElementById("cron-modal");
    const saveBtn = document.getElementById("cron-save");
    const { open: openModal, close: closeModal } = initModal(modal);
    
    const projectSelect = document.getElementById("cron-project-select");
    const testfileSelect = document.getElementById("cron-testfile-select");
    const exprInput = document.getElementById("cron-expr");

    let items = [];

    const loadJobs = async () => {
        try {
            const res = await fetch("/api/cron");
            if (!res.ok) throw new Error(await res.text());
            const data = await res.json();
            items = data.items || [];
            render();
        } catch (e) {
            console.error("Failed to load cron jobs:", e);
        }
    };

    const loadProjectsAndFiles = async () => {
        try {
            const [pRes, tRes] = await Promise.all([
                fetch("/api/projects"),
                fetch("/api/testfiles")
            ]);
            const pData = await pRes.json();
            const tData = await tRes.json();

            projectSelect.innerHTML = pData.items?.map(p => `<option value="${escapeHTML(p.name)}">${escapeHTML(p.name)}</option>`).join("") || "";
            
            const updateTestfiles = () => {
                const proj = projectSelect.value;
                const files = (tData.items || []).filter(t => t.project === proj);
                testfileSelect.innerHTML = `<option value="">(all tests in project)</option>` + 
                    files.map(f => `<option value="${escapeHTML(f.name)}">${escapeHTML(f.name)}</option>`).join("");
            };

            projectSelect.onchange = updateTestfiles;
            if (projectSelect.options.length > 0) {
                updateTestfiles();
            }
        } catch (e) {
            console.error("Failed to load projects for cron:", e);
        }
    };

    const render = () => {
        listEl.innerHTML = "";
        for (const j of items) {
            const tr = document.createElement("tr");
            
            const toggleId = `cron-toggle-${j.id}`;
            tr.innerHTML = `
                <td>${j.id}</td>
                <td>${escapeHTML(j.project)}</td>
                <td>${j.testfile ? escapeHTML(j.testfile) : '<em>(Project Run)</em>'}</td>
                <td><code>${escapeHTML(j.cron_expr)}</code></td>
                <td>
                    <input type="checkbox" id="${toggleId}" ${j.active ? 'checked' : ''}>
                </td>
                <td class="col-actions">
                    <button type="button" class="btn-action-del cron-del" data-id="${j.id}">Delete</button>
                </td>
            `;
            listEl.appendChild(tr);

            document.getElementById(toggleId).onchange = async (e) => {
                try {
                    await fetch("/api/cron/modify", {
                        method: "POST",
                        body: JSON.stringify({ id: j.id, active: e.target.checked })
                    });
                } catch (err) {
                    console.error("Toggle failed", err);
                    e.target.checked = !e.target.checked; // revert UI
                }
            };

            tr.querySelector(".cron-del").onclick = async () => {
                if (!confirm("Delete this cron job?")) return;
                try {
                    await fetch(`/api/cron/modify?id=${j.id}`, { method: "DELETE" });
                    loadJobs();
                } catch (err) {
                    console.error("Delete failed", err);
                }
            };
        }
    };

    refreshBtn.onclick = loadJobs;
    
    newBtn.onclick = async () => {
        await loadProjectsAndFiles();
        openModal();
    };

    saveBtn.onclick = async () => {
        const body = {
            project: projectSelect.value,
            testfile: testfileSelect.value,
            cron_expr: exprInput.value
        };

        if (!body.project) {
            alert("Project is required.");
            return;
        }

        try {
            const res = await fetch("/api/cron", {
                method: "POST",
                body: JSON.stringify(body)
            });
            if (!res.ok) throw new Error(await res.text());
            
            closeModal();
            loadJobs();
        } catch (e) {
            alert("Failed saving cron job: " + e.message);
        }
    };

    EventBus.on("cron:opened", () => {
        loadJobs();
    });
}

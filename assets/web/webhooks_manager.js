import { EventBus } from "./event_bus.js";

export function initWebhooksManager() {
    const refreshBtn = document.getElementById("webhooks-refresh");
    const newBtn = document.getElementById("webhooks-new");
    const listEl = document.getElementById("webhooks-list");

    const modal = document.getElementById("webhooks-modal");
    const modalTitle = document.getElementById("webhooks-modal-title");
    const saveBtn = document.getElementById("webhooks-save");
    const cancelBtn = document.getElementById("webhooks-cancel");

    const aliasInput = document.getElementById("webhooks-alias");
    const urlInput = document.getElementById("webhooks-url");
    const enabledInput = document.getElementById("webhooks-enabled");

    let items = [];
    let editingAlias = null;

    const loadWebhooks = async () => {
        try {
            const res = await fetch("/api/webhooks");
            if (!res.ok) throw new Error(await res.text());
            const data = await res.json();
            items = data.items || [];
            render();
        } catch (e) {
            console.error("Failed to load webhooks:", e);
        }
    };

    const render = () => {
        listEl.innerHTML = "";
        for (const wh of items) {
            const tr = document.createElement("tr");

            tr.innerHTML = `
                <td><strong>${wh.alias}</strong></td>
                <td><code>${wh.url}</code></td>
                <td><span class="webhook-status ${wh.enabled ? 'enabled' : 'disabled'}">${wh.enabled ? 'Enabled' : 'Disabled'}</span></td>
                <td>
                    <button type="button" class="webhooks-edit" data-alias="${wh.alias}">Edit</button>
                    <button type="button" class="webhooks-del" data-alias="${wh.alias}">Delete</button>
                </td>
            `;
            listEl.appendChild(tr);

            tr.querySelector(".webhooks-edit").onclick = () => {
                editingAlias = wh.alias;
                modalTitle.textContent = "Edit Webhook";
                aliasInput.value = wh.alias;
                urlInput.value = wh.url;
                enabledInput.checked = wh.enabled;
                modal.classList.add("active");
            };

            tr.querySelector(".webhooks-del").onclick = async () => {
                if (!confirm(`Delete webhook "${wh.alias}"?`)) return;
                try {
                    const res = await fetch(`/api/webhooks/delete?alias=${encodeURIComponent(wh.alias)}`, { method: "DELETE" });
                    if (!res.ok) throw new Error(await res.text());
                    loadWebhooks();
                } catch (err) {
                    console.error("Delete failed", err);
                    alert("Delete failed: " + err.message);
                }
            };
        }
    };

    refreshBtn.onclick = loadWebhooks;

    newBtn.onclick = () => {
        editingAlias = null;
        modalTitle.textContent = "Add Webhook";
        aliasInput.value = "";
        urlInput.value = "";
        enabledInput.checked = true;
        modal.classList.add("active");
    };

    cancelBtn.onclick = () => {
        modal.classList.remove("active");
    };

    saveBtn.onclick = async () => {
        const body = {
            old_alias: editingAlias,
            alias: aliasInput.value.trim(),
            url: urlInput.value.trim(),
            enabled: enabledInput.checked
        };

        if (!body.alias) {
            alert("Alias is required.");
            return;
        }
        if (!body.url) {
            alert("Webhook URL is required.");
            return;
        }

        try {
            const res = await fetch("/api/webhooks", {
                method: "POST",
                headers: {
                    "Content-Type": "application/json"
                },
                body: JSON.stringify(body)
            });
            if (!res.ok) throw new Error(await res.text());

            modal.classList.remove("active");
            loadWebhooks();
        } catch (e) {
            alert("Failed saving webhook: " + e.message);
        }
    };

    EventBus.on("webhooks:opened", () => {
        loadWebhooks();
    });
}

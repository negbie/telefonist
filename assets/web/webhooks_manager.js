import { escapeHTML, initModal } from "./utils.js";
import { EventBus } from "./event_bus.js";

export function initWebhooksManager() {
    const refreshBtn = document.getElementById("webhooks-refresh");
    const newBtn = document.getElementById("webhooks-new");
    const listEl = document.getElementById("webhooks-list");

    const modal = document.getElementById("webhooks-modal");
    const modalTitle = document.getElementById("webhooks-modal-title");
    const saveBtn = document.getElementById("webhooks-save");

    const aliasInput = document.getElementById("webhooks-alias");
    const urlInput = document.getElementById("webhooks-url");
    const enabledInput = document.getElementById("webhooks-enabled");

    const { open: openModal, close: closeModal } = initModal(modal);

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
                <td><strong title="${escapeHTML(wh.alias)}">${escapeHTML(wh.alias)}</strong></td>
                <td><code title="${escapeHTML(wh.url)}">${escapeHTML(wh.url)}</code></td>
                <td><span class="webhook-status ${wh.enabled ? 'enabled' : 'disabled'}">${wh.enabled ? 'Enabled' : 'Disabled'}</span></td>
                <td class="col-actions webhooks-actions">
                    <button type="button" class="btn-action-edit webhooks-edit" title="Edit webhook" data-alias="${escapeHTML(wh.alias)}">Edit</button>
                    <button type="button" class="btn-action-del webhooks-del" title="Delete webhook" data-alias="${escapeHTML(wh.alias)}">Delete</button>
                </td>
            `;
            listEl.appendChild(tr);

            tr.querySelector(".webhooks-edit").onclick = () => {
                editingAlias = wh.alias;
                modalTitle.textContent = "Edit Webhook";
                aliasInput.value = wh.alias;
                urlInput.value = wh.url;
                enabledInput.checked = wh.enabled;
                openModal();
            };

            tr.querySelector(".webhooks-del").onclick = async () => {
                if (!confirm(`Delete webhook "${escapeHTML(wh.alias)}"?`)) return;
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
        openModal();
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

            closeModal();
            loadWebhooks();
        } catch (e) {
            alert("Failed saving webhook: " + e.message);
        }
    };

    EventBus.on("webhooks:opened", () => {
        loadWebhooks();
    });
}

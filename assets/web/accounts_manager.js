import { EventBus } from "./event_bus.js";
import { API } from "./api.js";

export function initAccountsManager() {
    const refreshBtn = document.getElementById("accounts-refresh");
    const newBtn = document.getElementById("accounts-new");
    const listEl = document.getElementById("accounts-list");

    const modal = document.getElementById("accounts-modal");
    const modalTitle = document.getElementById("accounts-modal-title");
    const saveBtn = document.getElementById("accounts-save");
    const cancelBtn = document.getElementById("accounts-cancel");

    const nameInput = document.getElementById("accounts-name");
    const sipUriInput = document.getElementById("accounts-sip-uri");
    const passwordInput = document.getElementById("accounts-password");
    const uriParamsInput = document.getElementById("accounts-uri-params");
    const addrParamsInput = document.getElementById("accounts-addr-params");

    let items = [];
    let editingName = null;

    const loadAccounts = async () => {
        try {
            const res = await fetch("/api/accounts");
            if (!res.ok) throw new Error(await res.text());
            const data = await res.json();
            items = data.items || [];
            render();
        } catch (e) {
            console.error("Failed to load SIP accounts:", e);
        }
    };

    const render = () => {
        listEl.innerHTML = "";
        for (const acc of items) {
            const tr = document.createElement("tr");

            tr.innerHTML = `
                <td><strong>${acc.name}</strong></td>
                <td><code>${acc.sip_uri}</code></td>
                <td><span class="password-masked" title="Password status">${acc.has_password ? "••••••••" : "(no password)"}</span></td>
                <td><code>${acc.uri_params || ""}</code></td>
                <td><code>${acc.addr_params || ""}</code></td>
                <td>
                    <button type="button" class="accounts-edit" data-name="${acc.name}">Edit</button>
                    <button type="button" class="accounts-del" data-name="${acc.name}">Delete</button>
                </td>
            `;
            listEl.appendChild(tr);

            tr.querySelector(".accounts-edit").onclick = () => {
                editingName = acc.name;
                modalTitle.textContent = "Edit SIP Account";
                nameInput.value = acc.name;
                nameInput.disabled = false;
                sipUriInput.value = acc.sip_uri;
                passwordInput.value = "";
                uriParamsInput.value = acc.uri_params || "";
                addrParamsInput.value = acc.addr_params || "";
                modal.classList.add("active");
            };

            tr.querySelector(".accounts-del").onclick = async () => {
                if (!confirm(`Delete account "${acc.name}"?`)) return;
                try {
                    const res = await fetch(`/api/accounts/delete?name=${encodeURIComponent(acc.name)}`, { method: "DELETE" });
                    if (!res.ok) throw new Error(await res.text());
                    loadAccounts();
                } catch (err) {
                    console.error("Delete failed", err);
                    alert("Delete failed: " + err.message);
                }
            };
        }
    };

    refreshBtn.onclick = loadAccounts;

    newBtn.onclick = () => {
        editingName = null;
        modalTitle.textContent = "Add SIP Account";
        nameInput.value = "";
        nameInput.disabled = false;
        sipUriInput.value = "";
        passwordInput.value = "";
        uriParamsInput.value = "";
        addrParamsInput.value = "";
        modal.classList.add("active");
    };

    cancelBtn.onclick = () => {
        modal.classList.remove("active");
    };

    const normalizeParamString = (p) => {
        p = p.trim();
        if (!p) return "";
        p = p.replace(/;+/g, ';');
        if (!p.startsWith(';')) {
            p = ';' + p;
        }
        p = p.replace(/;+$/, '').trim();
        return p;
    };

    saveBtn.onclick = async () => {
        const body = {
            old_name: editingName,
            name: nameInput.value.trim(),
            sip_uri: sipUriInput.value.trim(),
            password: passwordInput.value,
            uri_params: normalizeParamString(uriParamsInput.value),
            addr_params: normalizeParamString(addrParamsInput.value)
        };

        if (!body.name) {
            alert("Name is required.");
            return;
        }
        if (!body.sip_uri) {
            alert("SIP URI is required.");
            return;
        }

        try {
            const res = await fetch("/api/accounts", {
                method: "POST",
                headers: {
                    "Content-Type": "application/json"
                },
                body: JSON.stringify(body)
            });
            if (!res.ok) throw new Error(await res.text());

            modal.classList.remove("active");
            loadAccounts();
        } catch (e) {
            alert("Failed saving SIP account: " + e.message);
        }
    };

    EventBus.on("accounts:opened", () => {
        loadAccounts();
    });
}

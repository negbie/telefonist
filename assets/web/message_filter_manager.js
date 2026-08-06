import { EventBus } from "./event_bus.js";

function splitFilters(value) {
  return (value || "")
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
}

export function initMessageFilterManager() {
  const refreshBtn = document.getElementById("message-filter-refresh");
  const saveBtn = document.getElementById("message-filter-save");
  const inputEl = document.getElementById("message-filter-input");
  const statusEl = document.getElementById("message-filter-status");

  if (!refreshBtn || !saveBtn || !inputEl) return;

  const setStatus = (message, isError = false) => {
    if (!statusEl) return;
    statusEl.textContent = message || "";
    statusEl.classList.toggle("error", !!isError);
  };

  const loadFilters = async () => {
    setStatus("Loading…");
    try {
      const res = await fetch("/api/settings/message-filter");
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      inputEl.value = (data.filters || []).join("\n");
      setStatus(`${(data.filters || []).length} filter(s) loaded.`);
    } catch (err) {
      console.error("Failed to load message filters:", err);
      setStatus("Failed to load message filters: " + err.message, true);
    }
  };

  const saveFilters = async () => {
    const filters = splitFilters(inputEl.value);
    setStatus("Saving…");
    try {
      const res = await fetch("/api/settings/message-filter", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ filters }),
      });
      if (!res.ok) throw new Error(await res.text());
      await res.json();
      inputEl.value = filters.join("\n");
      setStatus(`${filters.length} filter(s) saved.`);
    } catch (err) {
      console.error("Failed to save message filters:", err);
      setStatus("Failed to save message filters: " + err.message, true);
    }
  };

  refreshBtn.onclick = loadFilters;
  saveBtn.onclick = saveFilters;

  EventBus.on("message-filter:opened", () => {
    loadFilters();
  });
}

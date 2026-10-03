import { EventBus } from "./event_bus.js";

let cachedFilters = [];
let filtersLoaded = false;
let pendingLoad = null;

function normalizeFilters(filters) {
  const seen = new Set();
  return (filters || []).map(value => String(value).trim()).filter(value => {
    const key = value.toLowerCase();
    if (!key || seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

function setCachedFilters(filters) {
  cachedFilters = normalizeFilters(filters);
  filtersLoaded = true;
  EventBus.emit("log-filter:changed", [...cachedFilters]);
  return [...cachedFilters];
}

export function shouldFilterLog(value) {
  const text = String(value || "").toLowerCase();
  return Boolean(text) && cachedFilters.some(filter => text.includes(filter.toLowerCase()));
}

export function loadLogFilters(force = false) {
  if (filtersLoaded && !force) return Promise.resolve([...cachedFilters]);
  if (pendingLoad && !force) return pendingLoad;

  pendingLoad = fetch("/api/settings/log-filter")
    .then(async res => {
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      return setCachedFilters(data.filters || []);
    })
    .finally(() => { pendingLoad = null; });
  return pendingLoad;
}

function splitFilters(value) {
  return (value || "")
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
}

export function initLogFilterManager() {
  const refreshBtn = document.getElementById("log-filter-refresh");
  const saveBtn = document.getElementById("log-filter-save");
  const inputEl = document.getElementById("log-filter-input");
  const statusEl = document.getElementById("log-filter-status");

  if (!refreshBtn || !saveBtn || !inputEl) return;

  const setStatus = (message, isError = false) => {
    if (!statusEl) return;
    statusEl.textContent = message || "";
    statusEl.classList.toggle("error", !!isError);
  };

  const loadFilters = async () => {
    setStatus("Loading…");
    try {
      const filters = await loadLogFilters(true);
      inputEl.value = filters.join("\n");
      setStatus(`${filters.length} filter(s) loaded.`);
    } catch (err) {
      console.error("Failed to load log filters:", err);
      setStatus("Failed to load log filters: " + err.message, true);
    }
  };

  const saveFilters = async () => {
    const filters = splitFilters(inputEl.value);
    setStatus("Saving…");
    try {
      const res = await fetch("/api/settings/log-filter", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ filters }),
      });
      if (!res.ok) throw new Error(await res.text());
      await res.json();
      const savedFilters = setCachedFilters(filters);
      inputEl.value = savedFilters.join("\n");
      setStatus(`${savedFilters.length} filter(s) saved.`);
    } catch (err) {
      console.error("Failed to save log filters:", err);
      setStatus("Failed to save log filters: " + err.message, true);
    }
  };

  refreshBtn.onclick = loadFilters;
  saveBtn.onclick = saveFilters;

  EventBus.on("log-filter:opened", () => {
    loadFilters();
  });
}

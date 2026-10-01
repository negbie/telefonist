// Telefonist embedded web UI client script (flow-only).
//
// Expects the server to serve `/ws` as a WebSocket endpoint and `/` as the UI.
import { EventBus } from "./event_bus.js";
import {
  wsURL,
  setBodyFlowView,
  wireSearchFilter,
  applyOnlyTestsFilterFromCheckbox,
  applyOnlyResultsFilterFromCheckbox,
} from "./utils.js";
import { initResizer } from "./resizer.js";
import { clearAppendQueue } from "./dom.js";
import { API } from "./api.js";
import { createSocketHandler } from "./socket_handler.js";
import { createSequentialFlowRenderer } from "./flow.js";
import { renderSipEvent, initSipCompare } from "./sip_renderer.js";
import { renderLogEvent } from "./log_renderer.js";
import { initTestfileManager } from "./testfile_manager.js";
import { initCompareWindow } from "./compare_window.js";
import { initCronManager } from "./cron_manager.js";
import { initAccountsManager } from "./accounts_manager.js";
import { initWebhooksManager } from "./webhooks_manager.js";
import { initLogFilterManager } from "./log_filter_manager.js";
import { initHistoryManager } from "./history_manager.js";

const flowEl = document.getElementById("flow");
const clearEl = document.getElementById("clear");
const logoutEl = document.getElementById("logout");
const logViewEl = document.getElementById("log-view");
const sipViewEl = document.getElementById("sip-view");

const onlyTestsEl = document.getElementById("only-tests");
const autoScrollEl = document.getElementById("autoscroll");
const collapseAllEl = document.getElementById("collapse-all");

const resizer = document.getElementById("resizer");
const topRow = document.getElementById("top-row");
const bottomRow = document.getElementById("bottom-row");

initResizer(resizer, topRow, bottomRow);

let socket = null;

const getOptions = () => ({
  autoscroll: autoScrollEl ? !!autoScrollEl.checked : true,
  maxItems: 3333,
  collapseAll: collapseAllEl ? !!collapseAllEl.checked : false,
});

const wireCheckbox = (el, applyFn) => {
  if (!el || !applyFn) return;
  applyFn(el);
  el.onchange = () => applyFn(el);
};

setBodyFlowView();

const searchInput = document.getElementById("search");
if (searchInput) {
  wireSearchFilter(searchInput, document.body, ".list");
}

const searchSipInput = document.getElementById("search-sip");
if (searchSipInput && sipViewEl) {
  wireSearchFilter(searchSipInput, sipViewEl, ".sip-ladder-row");
}

const searchLogInput = document.getElementById("search-log");
if (searchLogInput && logViewEl) {
  wireSearchFilter(searchLogInput, logViewEl, ".log-row");
}

wireCheckbox(onlyTestsEl, applyOnlyTestsFilterFromCheckbox);

const onlyResultsEl = document.getElementById("only-results");
wireCheckbox(onlyResultsEl, applyOnlyResultsFilterFromCheckbox);

const flow =
  flowEl && createSequentialFlowRenderer
    ? createSequentialFlowRenderer(flowEl, getOptions)
    : null;

const clearMessages = () => {
  [flowEl, logViewEl, sipViewEl].forEach((el) => {
    if (!el) return;
    clearAppendQueue(el);
    el.innerHTML = "";
    el.scrollTop = 0;
    if (el === sipViewEl) {
      el._msgCount = 0;
      if (window.updateSipCompare) window.updateSipCompare();
    }
  });
};

if (clearEl) clearEl.onclick = clearMessages;

if (logoutEl) {
  logoutEl.onclick = () => {
    API.logout();
  };
}

// SIP Compare logic
const sipComparePanel = document.getElementById("sip-compare-panel");
const closeSipCompareBtn = document.getElementById("close-sip-compare");
if (initSipCompare) {
  initSipCompare({ sipViewEl, sipComparePanel, closeSipCompareBtn });
}

if (collapseAllEl && flow) {
  collapseAllEl.onchange = () => flow.setCollapseAll(!!collapseAllEl.checked);
}

const socketWrapper = {
  isOpen: () => socket && socket.isOpen(),
  send: (m) => {
    if (socket && socket.isOpen()) socket.send(m);
  },
};

const testfileInputEl = document.getElementById("testfile-input");
const testfilesRunEl = document.getElementById("testfiles-run");
const testfilesStopEl = document.getElementById("testfiles-stop");
const testfileSelectEl = document.getElementById("testfile-select");
const testfilesSaveEl = document.getElementById("testfiles-save");
const testfilesNewEl = document.getElementById("testfiles-new");
const testfilesRenameEl = document.getElementById("testfiles-rename");
const testfilesCloneEl = document.getElementById("testfiles-clone");
const testfilesDeleteEl = document.getElementById("testfiles-delete");
const testfileHighlightsEl = document.getElementById("testfile-highlights");

let tfManager = null;
if (initTestfileManager) {
  tfManager = initTestfileManager({
    socket: socketWrapper,
    testfileInputEl,
    testfilesRunEl,
    testfilesStopEl,
    testfileSelectEl,
    testfilesSaveEl,
    testfilesNewEl,
    testfilesRenameEl,
    testfilesCloneEl,
    testfilesDeleteEl,
    testfileHighlightsEl,
    renderError: (j) => {
      if (flow) flow.renderEvent(j);
    },
    onActiveFileChange: (key) => {
      const [project, name] = key ? key.split(":") : ["", ""];
      EventBus.emit("testfile:changed", name, project);
    },
  });
}

if (initCompareWindow) {
  initCompareWindow({ getActiveKey: () => tfManager?.getActiveKey() });
}

if (initHistoryManager) {
  initHistoryManager({
    getActiveKey: () => tfManager?.getActiveKey(),
    testfileInputEl,
  });
}

const btnModeTests = document.getElementById("btn-mode-tests");
const btnModeCompare = document.getElementById("btn-mode-compare");
const btnModeGit = document.getElementById("btn-mode-git");
const btnModeCron = document.getElementById("btn-mode-cron");
const btnModeSettings = document.getElementById("btn-mode-settings");

const switchSettingsSubPanel = (target) => {
  const panelName = target || "accounts";
  if (bottomRow) {
    bottomRow.setAttribute("data-settings-panel", panelName);
  }
  EventBus.emit(`${panelName}:opened`);
};

const setBottomMode = (mode) => {
  if (!bottomRow) return;

  bottomRow.setAttribute("data-bottom-mode", mode);

  if (btnModeTests) btnModeTests.classList.toggle("active", mode === "tests");
  if (btnModeCompare) btnModeCompare.classList.toggle("active", mode === "compare");
  if (btnModeGit) btnModeGit.classList.toggle("active", mode === "git");
  if (btnModeCron) btnModeCron.classList.toggle("active", mode === "cron");
  if (btnModeSettings) btnModeSettings.classList.toggle("active", mode === "settings");

  if (mode === "settings") {
    const activeItem = document.querySelector(".settings-tree-item.active");
    switchSettingsSubPanel(activeItem ? activeItem.getAttribute("data-target") : "accounts");
  } else if (mode === "compare") {
    EventBus.emit("compare:opened");
  } else if (mode === "git") {
    EventBus.emit("history:opened");
  } else if (mode === "cron") {
    EventBus.emit("cron:opened");
  }
};

if (btnModeTests) btnModeTests.onclick = () => setBottomMode("tests");
if (btnModeCompare) btnModeCompare.onclick = () => setBottomMode("compare");
if (btnModeGit) btnModeGit.onclick = () => setBottomMode("git");
if (btnModeCron) btnModeCron.onclick = () => setBottomMode("cron");
if (btnModeSettings) btnModeSettings.onclick = () => setBottomMode("settings");

// Bind Settings tree item clicks
document.querySelectorAll(".settings-tree-item").forEach(item => {
  item.onclick = () => {
    document.querySelectorAll(".settings-tree-item").forEach(el => el.classList.remove("active"));
    item.classList.add("active");
    switchSettingsSubPanel(item.getAttribute("data-target"));
  };
});

// Initialize Cron Manager
if (initCronManager) {
  initCronManager();
}

// Initialize Accounts Manager
if (initAccountsManager) {
  initAccountsManager();
}

// Initialize Webhooks Manager
if (initWebhooksManager) {
  initWebhooksManager();
}

// Initialize Log Filter Manager
if (initLogFilterManager) {
  initLogFilterManager();
}


// Sidebar Toggle Logic
const testControls = document.getElementById("test-controls");
const sidebarToggle = document.getElementById("sidebar-toggle");
if (sidebarToggle && testControls) {
  sidebarToggle.onclick = (e) => {
    e.stopPropagation();
    testControls.classList.toggle("expanded");
  };
}

// WebSocket Handler
document.getElementById("toggle-live").onclick = event => {
  const visible = document.body.classList.toggle("show-live");
  event.currentTarget.setAttribute("aria-pressed", String(visible));
};
let lastRunSummary = null;
let isRunnerActive = false;

function formatReason(msg) {
  if (!msg) return "";
  return msg.replace(/command rejected:\s*/i, "").trim();
}

function updateRunSummaryUI(state) {
  const el = document.getElementById("run-summary");
  if (!el) return;
  if (state?.active) {
    const batchPart = (state.file_total > 1 && state.file_index) ? `[${state.file_index}/${state.file_total}] ` : "";
    const repeatPart = (state.repeats > 1 && state.repeat) ? ` (${state.repeat}/${state.repeats})` : "";
    const prevPart = state.prev_result ? ` ─── Prev: ${state.prev_result}` : "";
    const nextPart = state.next_file ? ` ─── Next: ${state.next_file}` : "";

    el.textContent = `${batchPart}Running ${state.project || "Uncategorized"} / ${state.file || "Preparing"}${repeatPart} · step ${state.step || 0} of ${state.total || 0} · Run ${state.run_id || "pending"}${prevPart}${nextPart}`;
    el.className = "running";
    if (state.prev_result) {
      el.title = `Previous: ${state.prev_result}`;
    } else {
      el.removeAttribute("title");
    }
  } else if (lastRunSummary) {
    const s = lastRunSummary;
    const isError = s.result === "ERROR";
    const reasonText = formatReason(s.message);
    const reasonPart = reasonText ? `, Reason: ${reasonText}` : "";
    const hashPart = (!isError && s.hash) ? `, Hash: ${s.hash}` : "";
    const runPart = s.run_id ? `, Run: ${s.run_id}` : "";
    el.textContent = `${s.file} [${s.result}] ─── Project: ${s.project}${reasonPart}${hashPart}${runPart}`;
    el.className = (s.result || "").toLowerCase();
    if (s.message) {
      el.title = `Reason: ${s.message}`;
    } else {
      el.removeAttribute("title");
    }
  } else {
    el.textContent = "No active run";
    el.className = "";
    el.removeAttribute("title");
  }
}

socket = createSocketHandler(wsURL(), {
  onOpen: () => {
    document.getElementById("connection-state").textContent = "Connected";
    if (tfManager) {
      tfManager.requestTestfilesList();
      tfManager.updateSaveEnabled();
    }
    EventBus.emit("ws:open");
  },
  onClose: () => {
    document.getElementById("connection-state").textContent = "Disconnected · reconnecting…";
    if (tfManager) tfManager.updateSaveEnabled();
    EventBus.emit("ws:close");
  },
  onGap: message => {
    const warning = document.getElementById("live-warning");
    warning.hidden = false; warning.textContent = message;
  },
  onMessage: (j) => {
    if ((j.token === "testfile" || j.token === "test") && j.status === "finished" && (j.result || j.actual_hash || j.run_id)) {
      lastRunSummary = {
        file: j.file || j.name || "",
        project: j.project || "",
        result: (j.result || "FINISHED").toUpperCase(),
        hash: j.actual_hash || j.hash || "",
        run_id: j.run_id || "",
        message: j.message || j.failure_reason || "",
      };
      if (!isRunnerActive) {
        updateRunSummaryUI({ active: false });
      }
    }

    if (j.token === "runner") {
      const state = j.state || {};
      isRunnerActive = !!state.active;
      updateRunSummaryUI(state);
    }
    EventBus.emit("ws:message", j);

    const isStatusProgressLike =
      j.status === "running" ||
      j.status === "finished" ||
      j.status === "progress";
    const isTestfileOrProjectToken =
      j.token === "testfiles" || j.token === "projects";

    if (tfManager?.handleTestfilesMessage(j)) {
      if (isTestfileOrProjectToken) return;

      // Keep test lifecycle/status events visible in flow.
      if (!isStatusProgressLike && j.type !== "CMD") return;
    }

    const isTestRunStart =
      (j.token === "testfile" || j.token === "test") && j.status === "running";

    if (isTestRunStart && sipViewEl) {
      sipViewEl._msgCount = 0;
      sipViewEl
        .querySelectorAll(".sip-ladder-row.selected")
        .forEach((el) => el.classList.remove("selected"));

      if (window.updateSipCompare) {
        window.updateSipCompare();
      }
    }

    const renderContext = { logViewEl, sipViewEl, flowEl, searchLogInput };
    if (renderLogEvent?.(j, renderContext, getOptions)) return;
    if (renderSipEvent?.(j, { sipViewEl, searchSipInput }, getOptions)) {
      return;
    }

    if (flow) {
      flow.renderEvent(j);
    }
  },
});

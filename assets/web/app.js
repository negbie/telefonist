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
import { API } from "./api.js";
import { createSocketHandler } from "./socket_handler.js";
import { createSequentialFlowRenderer } from "./flow.js";
import { renderSipEvent, initSipCompare } from "./sip_renderer.js";
import { renderLogEvent } from "./log_renderer.js";
import { initTestfileManager } from "./testfile_manager.js";
import { initCompareWindow } from "./compare_window.js";

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

const btnModeTests = document.getElementById("btn-mode-tests");
const btnModeCompare = document.getElementById("btn-mode-compare");

const syncCompareWithActiveTestfile = () => {
  const key = tfManager?.getActiveKey?.() || "";
  const [project, name] = key ? key.split(":") : ["", ""];
  EventBus.emit("testfile:changed", name, project);
};

const setBottomMode = (mode) => {
  if (!bottomRow) return;

  bottomRow.setAttribute("data-bottom-mode", mode);

  if (btnModeTests) {
    btnModeTests.classList.toggle("active", mode === "tests");
  }
  if (btnModeCompare) {
    btnModeCompare.classList.toggle("active", mode === "compare");
  }

  if (mode === "compare") {
    syncCompareWithActiveTestfile();
  }
};

if (btnModeTests) btnModeTests.onclick = () => setBottomMode("tests");
if (btnModeCompare) btnModeCompare.onclick = () => setBottomMode("compare");

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
socket = createSocketHandler(wsURL(), {
  onOpen: () => {
    if (tfManager) {
      tfManager.requestTestfilesList();
      tfManager.updateSaveEnabled();
    }
    EventBus.emit("ws:open");
  },
  onClose: () => {
    if (tfManager) tfManager.updateSaveEnabled();
    EventBus.emit("ws:close");
  },
  onMessage: (j) => {
    EventBus.emit("ws:message", j);

    const isStatusProgressLike =
      j.status === "running" ||
      j.status === "finished" ||
      j.status === "progress";
    const isTestfileOrProjectToken =
      j.token === "testfiles" || j.token === "projects";

    if (tfManager?.handleTestfilesMessage(j)) {
      if (j.token === "testfiles" && j.name) {
        EventBus.emit("testfile:changed", j.name, j.project);
      }

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

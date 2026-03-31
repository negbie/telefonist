// Telefonist embedded web UI client script (flow-only).
//
// Expects the server to serve `/ws` as a WebSocket endpoint and `/` as the UI.
//
// Refactored to use utils.js, flow.js, visual_builder.js,
// sip_renderer.js, log_renderer.js, and testfile_manager.js

(function () {
  window.onload = function () {
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

    window.initResizer(resizer, topRow, bottomRow);

    let socket = null;

    const getOptions = () => ({
      autoscroll: autoScrollEl ? !!autoScrollEl.checked : true,
      maxItems: 3333,
      collapseAll: collapseAllEl ? !!collapseAllEl.checked : false,
    });

    setBodyFlowView();

    // Centralized search filtering
    const searchInput = document.getElementById("search");
    if (searchInput) {
      window.wireSearchFilter(searchInput, document.body, ".list");
    }

    const searchSipInput = document.getElementById("search-sip");
    if (searchSipInput && sipViewEl) {
      window.wireSearchFilter(searchSipInput, sipViewEl, ".sip-ladder-row");
    }

    const searchLogInput = document.getElementById("search-log");
    if (searchLogInput && logViewEl) {
      window.wireSearchFilter(searchLogInput, logViewEl, ".log-row");
    }

    function wireCheckbox(el, applyFn) {
      if (!el || !applyFn) return;
      applyFn(el);
      el.onchange = function () {
        applyFn(el);
      };
    }

    wireCheckbox(onlyTestsEl, applyOnlyTestsFilterFromCheckbox);

    const onlyResultsEl = document.getElementById("only-results");
    wireCheckbox(onlyResultsEl, applyOnlyResultsFilterFromCheckbox);

    const flow =
      flowEl && window.createSequentialFlowRenderer
        ? window.createSequentialFlowRenderer(flowEl, getOptions)
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
        if (window.API && window.API.logout) {
          window.API.logout();
        } else {
          window.location.href = "/login.html";
        }
      };
    }

    // SIP Compare logic
    const sipComparePanel = document.getElementById("sip-compare-panel");
    const closeSipCompareBtn = document.getElementById("close-sip-compare");
    if (window.initSipCompare) {
      window.initSipCompare({ sipViewEl, sipComparePanel, closeSipCompareBtn });
    }

    if (collapseAllEl && flow) {
      collapseAllEl.onchange = () =>
        flow.setCollapseAll(!!collapseAllEl.checked);
    }

    const socketWrapper = {
      isOpen: () => socket && socket.isOpen(),
      send: (m) => {
        if (socket && socket.isOpen()) socket.send(m);
      },
    };

    let tfManager = null;
    if (window.initTestfileManager) {
      tfManager = window.initTestfileManager({
        socket: socketWrapper,
        testfileInputEl: document.getElementById("testfile-input"),
        testfilesRunEl: document.getElementById("testfiles-run"),
        testfilesStopEl: document.getElementById("testfiles-stop"),
        testfileSelectEl: document.getElementById("testfile-select"),
        testfilesSaveEl: document.getElementById("testfiles-save"),
        testfilesNewEl: document.getElementById("testfiles-new"),
        testfilesRenameEl: document.getElementById("testfiles-rename"),
        testfilesDeleteEl: document.getElementById("testfiles-delete"),
        testfileHighlightsEl: document.getElementById("testfile-highlights"),
        renderError: (j) => {
          if (flow) flow.renderEvent(j);
        },
        onActiveFileChange: (key) => {
          const [project, name] = key ? key.split(":") : ["", ""];
          EventBus.emit("testfile:changed", name, project);
        },
      });
      window._tfManager = tfManager;
    }

    if (window.initCompareWindow) {
      window.initCompareWindow({});
    }

    const btnModeTests = document.getElementById("btn-mode-tests");
    const btnModeCompare = document.getElementById("btn-mode-compare");

    const setBottomMode = (mode) => {
      if (!bottomRow) return;
      bottomRow.setAttribute("data-bottom-mode", mode);
      if (btnModeTests)
        btnModeTests.classList.toggle("active", mode === "tests");
      if (btnModeCompare)
        btnModeCompare.classList.toggle("active", mode === "compare");
      if (mode === "compare") {
        const key = tfManager?.getActiveKey?.() || "";
        const [project, name] = key ? key.split(":") : ["", ""];
        EventBus.emit("testfile:changed", name, project);
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

        if (tfManager?.handleTestfilesMessage(j)) {
          if (j.token === "testfiles" && j.name) {
            EventBus.emit("testfile:changed", j.name, j.project);
          }
          // Only return if it's a pure testfile/projects management message.
          // Test status updates (running, finished, progress) should still be rendered in the flow.
          if (
            !j.status ||
            (j.status !== "running" &&
              j.status !== "finished" &&
              j.status !== "progress")
          ) {
            return;
          }
        }

        if (
          (j.token === "testfile" || j.token === "test") &&
          j.status === "running"
        ) {
          if (sipViewEl) {
            sipViewEl._msgCount = 0;
            sipViewEl
              .querySelectorAll(".sip-ladder-row.selected")
              .forEach((el) => el.classList.remove("selected"));
            if (window.updateSipCompare) window.updateSipCompare();
          }
        }

        const renderContext = { logViewEl, sipViewEl, flowEl, searchLogInput };
        if (window.renderLogEvent?.(j, renderContext, getOptions)) return;
        if (
          window.renderSipEvent?.(j, { sipViewEl, searchSipInput }, getOptions)
        )
          return;

        if (flow) flow.renderEvent(j);
      },
    });
  };
})();

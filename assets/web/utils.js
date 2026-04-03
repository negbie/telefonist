export function nowLocalString() {
  return new Date().toLocaleString();
}

export function safeText(value) {
  return value == null ? "" : String(value);
}

export function stripANSI(s) {
  return safeText(s).replace(
    /[\u001b\u009b][[()#;?]*(?:[0-9]{1,4}(?:;[0-9]{0,4})*)?[0-9A-ORZcf-nqry=><]/g,
    "",
  );
}

export function base64EncodeUTF8(s) {
  return btoa(
    encodeURIComponent(s).replace(/%([0-9A-F]{2})/g, function (_, p1) {
      return String.fromCharCode(parseInt(p1, 16));
    }),
  );
}

export function base64DecodeUTF8(b64) {
  var binary = atob(safeText(b64));
  var bytes = new Uint8Array(binary.length);
  for (var i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return new TextDecoder().decode(bytes);
}

export function sanitizeTestfileName(raw) {
  var s = safeText(raw || "").replace(/\s+/g, "");
  s = s.replace(/[^A-Za-z0-9._-]/g, "");
  return s.slice(0, 64);
}

export function escapeHTML(str) {
  return safeText(str)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");
}

export function wireSearchFilter(inputEl, containerEl, selector) {
  if (!inputEl || !containerEl) return;

  var lastFilter = null;

  function applyFilter() {
    var filter = safeText(inputEl.value).toLowerCase();
    if (filter === lastFilter) return;
    lastFilter = filter;

    var nodes = containerEl.querySelectorAll(selector);
    for (var i = 0; i < nodes.length; i++) {
      var node = nodes[i];
      var text = safeText(node.textContent).toLowerCase();
      node.style.display = text.includes(filter) ? "" : "none";
    }
  }

  inputEl.addEventListener("input", applyFilter);
  applyFilter();
}

export function wsURL() {
  var scheme = window.location.protocol === "https:" ? "wss://" : "ws://";
  return scheme + window.location.host + "/ws";
}

export function setBodyFlowView() {
  document.body.setAttribute("data-view", "flow");
}

export function setBodyToggleAttribute(checkboxEl, attrName) {
  if (!checkboxEl) return;
  if (checkboxEl.checked) {
    document.body.setAttribute(attrName, "1");
  } else {
    document.body.removeAttribute(attrName);
  }
}

export function applyOnlyTestsFilterFromCheckbox(onlyTestsEl) {
  setBodyToggleAttribute(onlyTestsEl, "data-only-tests");
}

export function applyOnlyResultsFilterFromCheckbox(onlyResultsEl) {
  setBodyToggleAttribute(onlyResultsEl, "data-only-results");
}

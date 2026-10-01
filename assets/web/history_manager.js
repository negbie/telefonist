import { escapeHTML, base64DecodeUTF8 } from "./utils.js";
import { computeLCSDiff } from "./diff.js";
import { EventBus } from "./event_bus.js";
import { API as api } from "./api.js";

function diffTexts(textA, textB) {
  const linesA = textA.split("\n").map((l) => ({ compare: l, display: l }));
  const linesB = textB.split("\n").map((l) => ({ compare: l, display: l }));
  return computeLCSDiff(linesA, linesB);
}

export function initHistoryManager(deps) {
  const { getActiveKey, testfileInputEl } = deps;

  const versionsList = document.getElementById("history-versions-list");
  const diffView = document.getElementById("history-diff-view");
  const restoreBtn = document.getElementById("history-restore");
  const historyTitle = document.getElementById("history-title");

  let selectedVersionContent = "";
  let currentTestfileName = "";
  let currentProjectName = "";

  const isGitModeActive = () => {
    return document.getElementById("bottom-row")?.getAttribute("data-bottom-mode") === "git";
  };

  let activeFetchController = null;
  const loadVersions = (name, project) => {
    currentTestfileName = name || "";
    currentProjectName = project || "";

    if (activeFetchController) {
      activeFetchController.abort();
    }

    if (!name) {
      if (historyTitle) historyTitle.textContent = "Git Version History";
      if (versionsList) versionsList.innerHTML = "<div class='history-empty'>Select a testfile from the left list to view Git history.</div>";
      if (diffView) diffView.innerHTML = "<div class='history-empty'>No testfile selected.</div>";
      if (restoreBtn) restoreBtn.style.display = "none";
      selectedVersionContent = "";
      return;
    }

    if (historyTitle) {
      const projLabel = project ? `${project} / ` : "";
      historyTitle.textContent = `Git Version History – ${projLabel}${name}`;
    }

    if (versionsList) versionsList.innerHTML = "<div class='history-empty'>Loading versions...</div>";
    if (diffView) diffView.innerHTML = "<div class='history-empty'>Select a version from the left list to view changes.</div>";
    if (restoreBtn) restoreBtn.style.display = "none";
    selectedVersionContent = "";

    activeFetchController = new AbortController();
    api.getTestfileVersions(name, project, activeFetchController.signal)
      .then((j) => {
        if (j.status !== "finished") {
          if (versionsList) versionsList.innerHTML = `<div class='history-empty'>Error: ${escapeHTML(j.message || "Failed to load versions")}</div>`;
          return;
        }

        const versions = j.versions || [];
        if (versions.length === 0) {
          if (versionsList) versionsList.innerHTML = "<div class='history-empty'>No previous saved versions found for this testfile.</div>";
          return;
        }

        if (versionsList) versionsList.innerHTML = "";
        versions.forEach((v, index) => {
          const item = document.createElement("div");
          item.className = "history-version-item";

          const textSpan = document.createElement("span");
          const date = new Date(v.created_at);
          const timeStr = date.toLocaleString();
          textSpan.textContent = `Version ${versions.length - index} (${timeStr})`;
          item.appendChild(textSpan);

          const delBtn = document.createElement("button");
          delBtn.type = "button";
          delBtn.className = "history-del-btn";
          delBtn.textContent = "✕";
          delBtn.title = "Delete this version entry";

          delBtn.onclick = (e) => {
            e.stopPropagation();
            if (!confirm("Are you sure you want to delete this version entry from history?")) return;
            api.deleteTestfileVersion(v.id)
              .then((res) => {
                if (res.status === "finished") {
                  loadVersions(currentTestfileName, currentProjectName);
                } else {
                  alert("Error deleting version: " + (res.message || "Failed to delete"));
                }
              })
              .catch((err) => alert("Error: " + err.message));
          };

          item.appendChild(delBtn);

          item.onclick = () => {
            document.querySelectorAll(".history-version-item").forEach((el) => el.classList.remove("selected"));
            item.classList.add("selected");

            const versionText = base64DecodeUTF8(v.content_b64);
            const currentText = testfileInputEl ? testfileInputEl.value : "";

            // Diff: versionText (textA) vs currentText (textB)
            const diff = diffTexts(versionText, currentText);

            if (diffView) {
              diffView.innerHTML = diff.map((d) => {
                if (d.type === "common") {
                  return `<div class="diff-line diff-line-common">  ${escapeHTML(d.text)}</div>`;
                } else if (d.type === "a") {
                  return `<div class="diff-line diff-line-del">- ${escapeHTML(d.text)}</div>`;
                } else {
                  return `<div class="diff-line diff-line-add">+ ${escapeHTML(d.text)}</div>`;
                }
              }).join("");
            }

            selectedVersionContent = versionText;
            if (restoreBtn) restoreBtn.style.display = "inline-block";
          };

          if (versionsList) versionsList.appendChild(item);
        });
      })
      .catch((err) => {
        if (versionsList) versionsList.innerHTML = `<div class='history-empty'>Network error: ${escapeHTML(err.message)}</div>`;
      });
  };

  EventBus.on("testfile:changed", (name, project) => {
    currentTestfileName = name || "";
    currentProjectName = project || "";
    if (isGitModeActive()) {
      loadVersions(name, project);
    }
  });

  EventBus.on("history:opened", () => {
    let activeKey = getActiveKey ? getActiveKey() : "";
    if (!activeKey) {
      const select = document.getElementById("testfile-select");
      if (select && select.value) {
        activeKey = select.value;
      }
    }
    const [project, name] = activeKey ? activeKey.split(":") : ["", ""];
    loadVersions(name, project);
  });

  if (restoreBtn) {
    restoreBtn.onclick = () => {
      if (!selectedVersionContent) return;
      if (!confirm("Are you sure you want to restore the selected version? Unsaved changes in the editor will be overwritten.")) return;

      if (testfileInputEl) {
        testfileInputEl.value = selectedVersionContent;
        testfileInputEl.dispatchEvent(new Event("input"));
      }
      alert("Selected version restored to editor.");
    };
  }
}

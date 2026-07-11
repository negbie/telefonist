import { escapeHTML, base64DecodeUTF8 } from "./utils.js";
import { computeLCSDiff } from "./diff.js";

function diffTexts(textA, textB) {
  const linesA = textA.split("\n").map((l) => ({ compare: l, display: l }));
  const linesB = textB.split("\n").map((l) => ({ compare: l, display: l }));
  return computeLCSDiff(linesA, linesB);
}

export function initHistoryManager(deps) {
  const { getActiveKey, testfileInputEl } = deps;

  const modal = document.getElementById("history-modal");
  const historyBtn = document.getElementById("testfiles-history");
  const versionsList = document.getElementById("history-versions-list");
  const diffView = document.getElementById("history-diff-view");
  const restoreBtn = document.getElementById("history-restore");
  const closeBtn = document.getElementById("history-close");

  let selectedVersionContent = "";

  if (!modal || !historyBtn) return;

  historyBtn.onclick = () => {
    const activeKey = getActiveKey();
    if (!activeKey) {
      alert("Please select a testfile first.");
      return;
    }

    const separatorIndex = activeKey.indexOf(":");
    const project = activeKey.substring(0, separatorIndex);
    const name = activeKey.substring(separatorIndex + 1);

    // Open modal
    modal.classList.add("active");
    versionsList.innerHTML = "<div class='history-empty'>Loading versions...</div>";
    diffView.innerHTML = "<div class='history-empty'>Select a version from the left list to compare and view changes.</div>";
    restoreBtn.style.display = "none";
    selectedVersionContent = "";

    fetch(`/api/testfile/versions?name=${encodeURIComponent(name)}&project=${encodeURIComponent(project)}`)
      .then((r) => r.json())
      .then((j) => {
        if (j.status !== "finished") {
          versionsList.innerHTML = `<div class='history-empty'>Error: ${escapeHTML(j.message || "Failed to load versions")}</div>`;
          return;
        }

        const versions = j.versions || [];
        if (versions.length === 0) {
          versionsList.innerHTML = "<div class='history-empty'>No previous saved versions found.</div>";
          return;
        }

        versionsList.innerHTML = "";
        versions.forEach((v, index) => {
          const item = document.createElement("div");
          item.className = "history-version-item";
          item.style.display = "flex";
          item.style.justifyContent = "space-between";
          item.style.alignItems = "center";
          
          const textSpan = document.createElement("span");
          const date = new Date(v.created_at);
          const timeStr = date.toLocaleString();
          textSpan.textContent = `Version ${versions.length - index} (${timeStr})`;
          item.appendChild(textSpan);

          const delBtn = document.createElement("span");
          delBtn.textContent = "✕";
          delBtn.style.color = "#991b1b";
          delBtn.style.fontWeight = "bold";
          delBtn.style.cursor = "pointer";
          delBtn.style.padding = "2px 6px";
          delBtn.style.borderRadius = "4px";
          delBtn.style.marginLeft = "10px";
          delBtn.title = "Delete this version entry";

          delBtn.onmouseover = () => delBtn.style.background = "#fee2e2";
          delBtn.onmouseout = () => delBtn.style.background = "none";

          delBtn.onclick = (e) => {
            e.stopPropagation();
            if (!confirm("Are you sure you want to delete this version entry from history?")) return;
            fetch(`/api/testfile/versions?id=${v.id}`, { method: "DELETE" })
              .then((r) => r.json())
              .then((res) => {
                if (res.status === "finished") {
                  historyBtn.click(); // reload list
                } else {
                  alert("Error deleting version: " + res.message);
                }
              })
              .catch((err) => alert("Network error: " + err.message));
          };

          item.appendChild(delBtn);
          
          item.onclick = () => {
            // Select version
            document.querySelectorAll(".history-version-item").forEach((el) => el.classList.remove("selected"));
            item.classList.add("selected");

            const versionText = base64DecodeUTF8(v.content_b64);
            const currentText = testfileInputEl ? testfileInputEl.value : "";
            
            // Diff: versionText (textA) vs currentText (textB)
            const diff = diffTexts(versionText, currentText);

            diffView.innerHTML = diff.map((d) => {
              if (d.type === "common") {
                return `<div class="diff-line diff-line-common">  ${escapeHTML(d.text)}</div>`;
              } else if (d.type === "a") {
                return `<div class="diff-line diff-line-del">- ${escapeHTML(d.text)}</div>`;
              } else {
                return `<div class="diff-line diff-line-add">+ ${escapeHTML(d.text)}</div>`;
              }
            }).join("");

            selectedVersionContent = versionText;
            restoreBtn.style.display = "inline-block";
          };

          versionsList.appendChild(item);
        });
      })
      .catch((err) => {
        versionsList.innerHTML = `<div class='history-empty'>Network error: ${escapeHTML(err.message)}</div>`;
      });
  };

  const closeModal = () => {
    modal.classList.remove("active");
  };

  if (closeBtn) closeBtn.onclick = closeModal;

  // Close modal when clicking outside content area
  modal.onclick = (e) => {
    if (e.target === modal) closeModal();
  };

  if (restoreBtn) {
    restoreBtn.onclick = () => {
      if (!selectedVersionContent) return;
      if (!confirm("Are you sure you want to restore the selected version? Unsaved changes in the editor will be overwritten.")) return;
      
      if (testfileInputEl) {
        testfileInputEl.value = selectedVersionContent;
        testfileInputEl.dispatchEvent(new Event("input"));
      }
      closeModal();
    };
  }
}

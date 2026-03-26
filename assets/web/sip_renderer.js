// sip_renderer.js

window.renderSipEvent = function (j, elements, getOptions) {
    var sipViewEl = elements.sipViewEl;
    var searchSipInput = elements.searchSipInput;

    if (j.type === "SIP" && sipViewEl) {
        var el = document.createElement("div");
        el.className = "sip-ladder-row";

        var param = j.param || "";
        var lines = param.split("\n");

        sipViewEl._msgCount = (sipViewEl._msgCount || 0) + 1;

        if (lines.length >= 2) {
            var fullTimeStr = safeText(lines[0].replace("#", ""));
            var parts = fullTimeStr.split("|");
            var timeStr = parts[0];
            var dir = parts.length > 1 ? parts[1] : "TX";

            var arrowMatch = safeText(lines[1]).match(/(\w+) ([0-9\.\:a-fA-F\[\]]+) -> ([0-9\.\:a-fA-F\[\]]+)/);
            var rest = safeText(lines.slice(2).join("\n"));
            var methodLine = safeText(lines[2] || "");

            var topText = methodLine;
            var bottomText = "";

            // Extract method from CSeq
            var cseqMatch = rest.match(/CSeq:\s*\d+\s+([A-Z]+)/);
            if (cseqMatch) {
                topText = cseqMatch[1];
                if (rest.startsWith("SIP/2.0")) {
                    var statusCode = rest.match(/SIP\/2\.0\s+(\d+)\s+(.*)/);
                    if (statusCode) {
                        bottomText = statusCode[1] + " " + statusCode[2];
                    }
                }
            } else if (!arrowMatch) {
                topText = safeText(lines[1]);
            }

            var src = "Unknown", dst = "Unknown";
            if (arrowMatch) {
                src = arrowMatch[2];
                dst = arrowMatch[3];
            }

            var isResponse = rest.startsWith("SIP/2.0");
            var methodColor = isResponse ? (rest.includes(" 200") ? "#16a34a" : (rest.includes(" 100") || rest.includes(" 180") || rest.includes(" 183") ? "#0284c7" : "#dc2626")) : "#000";

            var header = document.createElement("div");
            header.className = "sip-ladder-header";
            header.onclick = function () { el.classList.toggle("open"); };

            var arrowCont = document.createElement("div");
            arrowCont.className = "sip-arrow-container";

            var localNodeEl = document.createElement("div");
            localNodeEl.className = "sip-node";
            localNodeEl.style.textAlign = "center";
            var remoteNodeEl = document.createElement("div");
            remoteNodeEl.className = "sip-node";
            remoteNodeEl.style.textAlign = "center";

            var srcNodeHtml = src + '<br><span style="font-size: 9px; color: #444; font-weight: normal;">' + timeStr + '</span>';
            var dstNodeHtml = dst + '<br><span style="font-size: 9px; color: #444; font-weight: normal;">' + timeStr + '</span>';

            if (dir === "TX") {
                localNodeEl.innerHTML = srcNodeHtml;
                localNodeEl.title = src;
                remoteNodeEl.innerHTML = dstNodeHtml;
                remoteNodeEl.title = dst;
            } else {
                localNodeEl.innerHTML = dstNodeHtml;
                localNodeEl.title = dst;
                remoteNodeEl.innerHTML = srcNodeHtml;
                remoteNodeEl.title = src;
            }

            var lineEl = document.createElement("div");
            lineEl.className = "sip-arrow-line";

            var methodTopEl = document.createElement("div");
            methodTopEl.className = "sip-method-top";
            methodTopEl.style.color = methodColor;
            
            var methodText = document.createElement("span");
            methodText.textContent = "(" + sipViewEl._msgCount + ") " + topText.substring(0, 60) + (topText.length > 60 ? "..." : "");
            methodTopEl.appendChild(methodText);

            var actions = document.createElement("div");
            actions.className = "sip-actions";

            var compareBtn = document.createElement("button");
            compareBtn.className = "btn-sip btn-compare";
            compareBtn.textContent = "Compare";
            compareBtn.onclick = function (e) {
                e.stopPropagation();
                var selected = sipViewEl.querySelectorAll(".sip-ladder-row.selected");
                if (el.classList.contains("selected")) {
                    el.classList.remove("selected");
                    compareBtn.classList.remove("compare-active");
                } else {
                    if (selected.length >= 2) {
                        var first = selected[0];
                        first.classList.remove("selected");
                        var firstBtn = first.querySelector(".btn-compare");
                        if (firstBtn) firstBtn.classList.remove("compare-active");
                    }
                    el.classList.add("selected");
                    compareBtn.classList.add("compare-active");
                }
                if (window.updateSipCompare) window.updateSipCompare();
            };
            actions.appendChild(compareBtn);
            methodTopEl.appendChild(actions);

            var methodBotEl = null;
            if (bottomText) {
                methodBotEl = document.createElement("div");
                methodBotEl.className = "sip-method-bottom";
                methodBotEl.style.color = methodColor;
                methodBotEl.textContent = bottomText.substring(0, 60) + (bottomText.length > 60 ? "..." : "");
            }

            var headEl = document.createElement("div");
            headEl.style.position = "absolute";
            headEl.style.top = "-4px";
            headEl.style.borderTop = "5px solid transparent";
            headEl.style.borderBottom = "5px solid transparent";
            if (dir === "TX") {
                headEl.style.right = "-2px";
                headEl.style.borderLeft = "6px solid #000";
            } else {
                headEl.style.left = "-2px";
                headEl.style.borderRight = "6px solid #000";
            }

            lineEl.appendChild(methodTopEl);
            if (methodBotEl) lineEl.appendChild(methodBotEl);
            lineEl.appendChild(headEl);

            arrowCont.appendChild(localNodeEl);
            arrowCont.appendChild(lineEl);
            arrowCont.appendChild(remoteNodeEl);

            header.appendChild(arrowCont);

            var details = document.createElement("pre");
            details.className = "sip-details";
            details.textContent = rest;

            el.appendChild(header);
            el.appendChild(details);
        } else {
            el.textContent = param;
            el.style.padding = "10px";
        }

        if (searchSipInput && searchSipInput.value) {
            var filter = searchSipInput.value.toLowerCase();
            if (!el.textContent.toLowerCase().includes(filter)) {
                el.style.display = "none";
            }
        }

        // Store unformatted text for comparison
        el._sipData = {
           raw: rest, 
           method: topText,
           seq: sipViewEl._msgCount
        };

        sipViewEl.appendChild(el);
        trimChildrenToMax(sipViewEl, getOptions().maxItems);
        if (getOptions().autoscroll) {
            sipViewEl.scrollTop = sipViewEl.scrollHeight;
        }
        return true; // handled
    }

    return false;
};

window.initSipCompare = function (elements) {
    const { sipViewEl, sipComparePanel, closeSipCompareBtn } = elements;

    if (closeSipCompareBtn) {
        closeSipCompareBtn.onclick = () => {
            if (sipViewEl) {
                const selected = sipViewEl.querySelectorAll(".sip-ladder-row.selected");
                selected.forEach(el => {
                    el.classList.remove("selected");
                    const btn = el.querySelector(".btn-compare");
                    if (btn) btn.classList.remove("compare-active");
                });
            }
            if (sipComparePanel) sipComparePanel.style.display = "none";
        };
    }

    window.updateSipCompare = () => {
        if (!sipViewEl || !sipComparePanel) return;
        const selected = sipViewEl.querySelectorAll(".sip-ladder-row.selected");
        sipComparePanel.style.display = selected.length ? "flex" : "none";
        if (!selected.length) return;

        [
            { side: "left", c: "sip-compare-content-left", d: selected[0]?._sipData },
            { side: "right", c: "sip-compare-content-right", d: selected[1]?._sipData }
        ].forEach(s => {
            const hEl = document.getElementById(`sip-compare-${s.side}`)?.querySelector('.sip-ladder-header');
            const cEl = document.getElementById(s.c);
            if (hEl) hEl.textContent = s.d ? "(" + s.d.seq + ") " + s.d.method : "";
            if (cEl) {
                cEl.textContent = s.d ? s.d.raw : "";
                if (cEl.parentElement) cEl.parentElement.style.display = s.d ? "flex" : "none";
            }
        });
    };
};

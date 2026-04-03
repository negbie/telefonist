import { safeText, escapeHTML } from "./utils.js";

function escapeRegExp(value) {
  return value.replace(/[-\/\\^$*+?.()|[\]{}]/g, "\\$&");
}

export function syntaxHighlight(inputEl, highlightsEl) {
  if (!inputEl || !highlightsEl) return;

  var text = safeText(inputEl.value);
  var lines = text.split("\n");
  var html = "";

  var palette = [
    "hsl(210, 75%, 40%)", // Blue
    "hsl(120, 65%, 35%)", // Green
    "hsl(0, 75%, 40%)", // Red
    "hsl(25, 85%, 40%)", // Orange
    "hsl(265, 70%, 45%)", // Indigo
    "hsl(50, 90%, 35%)", // Yellow-ish
    "hsl(300, 65%, 40%)", // Violet
  ];

  var vars = {};
  var colorIdx = 0;
  for (var i = 0; i < lines.length; i++) {
    var trimmed = lines[i].trim();
    if (trimmed.startsWith("_define ")) {
      var parts = trimmed.split(/\s+/);
      if (parts.length >= 2) {
        var name = parts[1];
        if (!vars[name]) {
          vars[name] = palette[colorIdx % palette.length];
          colorIdx++;
        }
      }
    }
  }

  function wrapVarToken(token) {
    return (
      '<span class="hl-var" style="color: ' +
      vars[token] +
      '; font-weight: bold;">' +
      escapeHTML(token) +
      "</span>"
    );
  }

  function highlightVars(rawText) {
    var escaped = escapeHTML(rawText);
    var varNames = Object.keys(vars);
    if (varNames.length === 0) return escaped;

    varNames.sort(function (a, b) {
      return b.length - a.length;
    });

    var pattern = new RegExp(
      "\\b(" + varNames.map(escapeRegExp).join("|") + ")\\b",
      "g",
    );

    return escaped.replace(pattern, function (matched) {
      return wrapVarToken(matched);
    });
  }

  for (var j = 0; j < lines.length; j++) {
    var line = lines[j];
    var trimmed = line.trim();

    if (trimmed.startsWith("#")) {
      html += '<span class="hl-comment">' + escapeHTML(line) + "</span>";
    } else if (trimmed.startsWith("_")) {
      var parts = line.match(/^(\s*)(_[a-zA-Z0-9]+)(\s+)(.*)$/);
      if (parts) {
        if (parts[1]) html += '<span class="hl-space">' + parts[1] + "</span>";
        html +=
          '<span class="hl-directive">' + escapeHTML(parts[2]) + "</span>";
        html += '<span class="hl-space">' + parts[3] + "</span>";

        if (parts[2] === "_define") {
          var valParts = parts[4].match(/^(\S+)(\s+)(.*)$/);
          if (valParts) {
            html += wrapVarToken(valParts[1]);
            html += '<span class="hl-space">' + valParts[2] + "</span>";
            html +=
              '<span class="hl-value">' + escapeHTML(valParts[3]) + "</span>";
          } else {
            html += wrapVarToken(parts[4]);
          }
        } else {
          html +=
            '<span class="hl-value">' + highlightVars(parts[4]) + "</span>";
        }
      } else {
        html += '<span class="hl-directive">' + escapeHTML(line) + "</span>";
      }
    } else {
      var rem = line;
      var nameMatch = rem.match(/^(\s*)([a-zA-Z0-9_.-]+:)(.*)$/);
      if (nameMatch) {
        if (nameMatch[1])
          html += '<span class="hl-space">' + nameMatch[1] + "</span>";
        html +=
          '<span class="hl-name">' + highlightVars(nameMatch[2]) + "</span>";
        rem = nameMatch[3] || "";
      }

      if (rem) {
        var pSplit = rem.split("|");
        for (var p = 0; p < pSplit.length; p++) {
          if (p > 0) html += '<span class="hl-pipe">|</span>';
          var tkMatch = pSplit[p].match(/^(\s*)(.*?)(\s*)$/);
          if (tkMatch) {
            if (tkMatch[1])
              html += '<span class="hl-space">' + tkMatch[1] + "</span>";
            var tk = tkMatch[2];
            if (tk) {
              html += '<span class="hl-cmd">' + highlightVars(tk) + "</span>";
            }
            if (tkMatch[3])
              html += '<span class="hl-space">' + tkMatch[3] + "</span>";
          }
        }
      }
    }
    if (j < lines.length - 1) html += "\n";
  }

  if (text.endsWith("\n")) html += " ";
  highlightsEl.innerHTML = html;
}

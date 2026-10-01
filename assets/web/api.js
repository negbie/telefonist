function redirectToLogin() {
  window.location.href = "/login.html";
}

function toQueryString(params) {
  if (!params) return "";
  var pairs = [];

  for (var key in params) {
    if (!Object.prototype.hasOwnProperty.call(params, key)) continue;
    var value = params[key];
    if (value === undefined || value === null) continue;
    pairs.push(
      encodeURIComponent(key) + "=" + encodeURIComponent(String(value)),
    );
  }

  return pairs.length ? "?" + pairs.join("&") : "";
}

function request(path, options) {
  return fetch(path, options).then(function (response) {
    if (response.status === 401) {
      redirectToLogin();
      throw new Error("Unauthorized");
    }

    return response.text().then(text => {
      let body;
      try { body = JSON.parse(text); } catch (_) {
        if (!response.ok) throw new Error("HTTP " + response.status + ": " + text.slice(0,300));
        throw new Error("HTTP " + response.status + ": invalid server response");
      }
      if (!response.ok) throw new Error(body.message || "HTTP " + response.status);
      return body;
    });
  });
}

function requestJSON(path, method, payload) {
  return request(path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export const API = {
  getProjects() {
    return request("/api/projects");
  },
  createProject(name) {
    return requestJSON("/api/projects", "POST", { name });
  },
  deleteProject(name) {
    return request("/api/projects" + toQueryString({ name }), {
      method: "DELETE",
    });
  },
  renameProject(oldName, newName) {
    return requestJSON("/api/projects/rename", "POST", {
      old_name: oldName,
      new_name: newName,
    });
  },
  cloneProject(srcName, targetName) {
    return requestJSON("/api/projects/clone", "POST", {
      src_name: srcName,
      target_name: targetName,
    });
  },

  getTestfiles() {
    return request("/api/testfiles");
  },
  getTestfile(name, project) {
    return request("/api/testfile" + toQueryString({ name, project }));
  },
  saveTestfile(name, project, contentB64, revision) {
    const payload = {
      name,
      project,
      content_b64: contentB64,
    };
    if (typeof revision === "string" && revision) {
      payload.revision = revision;
    }
    return requestJSON("/api/testfile", "POST", payload);
  },
  deleteTestfile(name, project) {
    return request("/api/testfile" + toQueryString({ name, project }), {
      method: "DELETE",
    });
  },
  renameTestfile(oldName, oldProject, newName, newProject) {
    return requestJSON("/api/testfile/rename", "POST", {
      old_name: oldName,
      old_project: oldProject,
      new_name: newName,
      new_project: newProject,
    });
  },
  cloneTestfile(project, srcName, targetName) {
    return requestJSON("/api/testfiles/clone", "POST", {
      project: project,
      src_name: srcName,
      target_name: targetName,
    });
  },

  logout() {
    return fetch("/api/logout", { method: "POST" }).then(function () {
      redirectToLogin();
    });
  },
};

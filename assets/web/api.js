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

    return response.json();
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
  saveTestfile(name, project, contentB64) {
    return requestJSON("/api/testfile", "POST", {
      name,
      project,
      content_b64: contentB64,
    });
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

  logout() {
    return fetch("/api/logout", { method: "POST" }).then(function () {
      redirectToLogin();
    });
  },
};

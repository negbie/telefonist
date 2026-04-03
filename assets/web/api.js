/* api.js */
/* api.js */
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
    method: method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

function get(path, query) {
  return request(path + toQueryString(query));
}

function del(path, query) {
  return request(path + toQueryString(query), { method: "DELETE" });
}

function post(path, payload) {
  return requestJSON(path, "POST", payload);
}

export const API = {
  // Projects
  getProjects: function () {
    return get("/api/projects");
  },
  createProject: function (name) {
    return post("/api/projects", { name: name });
  },
  deleteProject: function (name) {
    return del("/api/projects", { name: name });
  },
  renameProject: function (oldName, newName) {
    return post("/api/projects/rename", {
      old_name: oldName,
      new_name: newName,
    });
  },
  cloneProject: function (srcName, targetName) {
    return post("/api/projects/clone", {
      src_name: srcName,
      target_name: targetName,
    });
  },

  // Testfiles
  getTestfiles: function () {
    return get("/api/testfiles");
  },
  getTestfile: function (name, project) {
    return get("/api/testfile", { name: name, project: project });
  },
  saveTestfile: function (name, project, contentB64) {
    return post("/api/testfile", {
      name: name,
      project: project,
      content_b64: contentB64,
    });
  },
  deleteTestfile: function (name, project) {
    return del("/api/testfile", { name: name, project: project });
  },
  renameTestfile: function (oldName, oldProject, newName, newProject) {
    return post("/api/testfile/rename", {
      old_name: oldName,
      old_project: oldProject,
      new_name: newName,
      new_project: newProject,
    });
  },

  // Auth
  logout: function () {
    return fetch("/api/logout", { method: "POST" }).then(function () {
      redirectToLogin();
    });
  },
};


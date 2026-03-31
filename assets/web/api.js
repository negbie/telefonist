/* api.js */
(function () {
  function handleResponse(response) {
    if (response.status === 401) {
      window.location.href = "/login.html";
      throw new Error("Unauthorized");
    }
    return response.json();
  }

  function request(url, options) {
    return fetch(url, options).then(handleResponse);
  }

  window.API = {
    getProjects: function () {
      return request("/api/projects");
    },
    createProject: function (name) {
      return request("/api/projects", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: name }),
      });
    },
    deleteProject: function (name) {
      return request("/api/projects?name=" + encodeURIComponent(name), {
        method: "DELETE",
      });
    },
    renameProject: function (oldName, newName) {
      return request("/api/projects/rename", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ old_name: oldName, new_name: newName }),
      });
    },
    cloneProject: function (srcName, targetName) {
      return request("/api/projects/clone", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ src_name: srcName, target_name: targetName }),
      });
    },
    getTestfiles: function () {
      return request("/api/testfiles");
    },
    getTestfile: function (name, project) {
      return request(
        "/api/testfile?name=" +
          encodeURIComponent(name) +
          "&project=" +
          encodeURIComponent(project),
      );
    },
    saveTestfile: function (name, project, contentB64) {
      return request("/api/testfile", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: name,
          project: project,
          content_b64: contentB64,
        }),
      });
    },
    deleteTestfile: function (name, project) {
      return request(
        "/api/testfile?name=" +
          encodeURIComponent(name) +
          "&project=" +
          encodeURIComponent(project),
        {
          method: "DELETE",
        },
      );
    },
    renameTestfile: function (oldName, oldProject, newName, newProject) {
      return request("/api/testfile/rename", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          old_name: oldName,
          old_project: oldProject,
          new_name: newName,
          new_project: newProject,
        }),
      });
    },
    logout: function () {
      return fetch("/api/logout", { method: "POST" }).then(function () {
        window.location.href = "/login.html";
      });
    },
  };
})();

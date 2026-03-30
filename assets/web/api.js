/* api.js */
(function() {
    function handleResponse(response) {
        if (response.status === 401) {
            window.location.href = "/login.html";
            throw new Error("Unauthorized");
        }
        return response.json();
    }

    window.API = {
        getProjects: function () {
            return fetch("/api/projects").then(handleResponse);
        },
        createProject: function (name) {
            return fetch("/api/projects", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ name: name })
            }).then(handleResponse);
        },
        deleteProject: function (name) {
            return fetch("/api/projects?name=" + encodeURIComponent(name), {
                method: "DELETE"
            }).then(handleResponse);
        },
        renameProject: function (oldName, newName) {
            return fetch("/api/projects/rename", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ old_name: oldName, new_name: newName })
            }).then(handleResponse);
        },
        cloneProject: function (srcName, targetName) {
            return fetch("/api/projects/clone", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ src_name: srcName, target_name: targetName })
            }).then(handleResponse);
        },
        getTestfiles: function () {
            return fetch("/api/testfiles").then(handleResponse);
        },
        getTestfile: function (name, project) {
            return fetch("/api/testfile?name=" + encodeURIComponent(name) + "&project=" + encodeURIComponent(project))
                .then(handleResponse);
        },
        saveTestfile: function (name, project, contentB64) {
            return fetch("/api/testfile", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ name: name, project: project, content_b64: contentB64 })
            }).then(handleResponse);
        },
        deleteTestfile: function (name, project) {
            return fetch("/api/testfile?name=" + encodeURIComponent(name) + "&project=" + encodeURIComponent(project), {
                method: "DELETE"
            }).then(handleResponse);
        },
        renameTestfile: function (oldName, oldProject, newName, newProject) {
            return fetch("/api/testfile/rename", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ old_name: oldName, old_project: oldProject, new_name: newName, new_project: newProject })
            }).then(handleResponse);
        },
        logout: function () {
            return fetch("/api/logout", { method: "POST" }).then(() => {
                window.location.href = "/login.html";
            });
        }
    };
})();

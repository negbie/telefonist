/* state_manager.js */
window.StateManager = function () {
    var fileCache = {}; // "project:name" -> { name, project, original, current }

    function getKeys() {
        return Object.keys(fileCache);
    }

    function isDirty(key) {
        if (!fileCache[key]) return false;
        var cur = (fileCache[key].current || "").replace(/\r/g, "");
        var orig = (fileCache[key].original || "").replace(/\r/g, "");
        return cur !== orig;
    }

    function updateCache(key, data) {
        if (!fileCache[key]) {
            fileCache[key] = {
                name: data.name,
                project: data.project,
                original: data.original || "",
                current: data.current || data.original || ""
            };
        } else {
            if (data.original !== undefined) fileCache[key].original = data.original;
            if (data.current !== undefined) fileCache[key].current = data.current;
        }
    }

    function pruneStale(serverKeys) {
        var serverMap = {};
        serverKeys.forEach(k => serverMap[k] = true);

        for (var key in fileCache) {
            if (!serverMap[key] && !isDirty(key)) {
                delete fileCache[key];
            }
        }
    }

    return {
        getCache: () => fileCache,
        getEntry: (key) => fileCache[key],
        getKeys: getKeys,
        isDirty: isDirty,
        updateCache: updateCache,
        pruneStale: pruneStale,
        deleteEntry: (key) => delete fileCache[key]
    };
};

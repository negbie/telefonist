export function createStateManager() {
  var fileCache = {}; // "project:name" -> { name, project, original, current }

  function normalizeText(value) {
    return String(value || "").replace(/\r/g, "");
  }

  function keyExists(key) {
    return Object.prototype.hasOwnProperty.call(fileCache, key);
  }

  function getKeys() {
    return Object.keys(fileCache);
  }

  function getEntry(key) {
    return fileCache[key];
  }

  function isDirty(key) {
    var entry = getEntry(key);
    if (!entry) return false;
    return normalizeText(entry.current) !== normalizeText(entry.original);
  }

  function updateCache(key, data) {
    var existing = getEntry(key);

    if (!existing) {
      fileCache[key] = {
        name: data.name,
        project: data.project,
        original: data.original || "",
        current:
          data.current !== undefined ? data.current : data.original || "",
      };
      return;
    }

    if (data.original !== undefined) existing.original = data.original;
    if (data.current !== undefined) existing.current = data.current;
    if (data.name !== undefined) existing.name = data.name;
    if (data.project !== undefined) existing.project = data.project;
  }

  function pruneStale(serverKeys) {
    var keep = Object.create(null);

    for (var i = 0; i < serverKeys.length; i++) {
      keep[serverKeys[i]] = true;
    }

    var keys = getKeys();
    for (var j = 0; j < keys.length; j++) {
      var key = keys[j];
      if (!keep[key] && !isDirty(key)) {
        delete fileCache[key];
      }
    }
  }

  function deleteEntry(key) {
    if (!keyExists(key)) return false;
    return delete fileCache[key];
  }

  return {
    getCache: function () {
      return fileCache;
    },
    getEntry: getEntry,
    getKeys: getKeys,
    isDirty: isDirty,
    updateCache: updateCache,
    pruneStale: pruneStale,
    deleteEntry: deleteEntry,
  };
}


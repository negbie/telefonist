export function createStateManager() {
  var fileCache = Object.create(null); // project:name -> editor snapshot
  const storageKey = "telefonist:unsaved-drafts:v1";
  try {
    if (typeof sessionStorage !== "undefined") {
      const drafts = JSON.parse(sessionStorage.getItem(storageKey) || "{}");
      for (const [key,value] of Object.entries(drafts)) {
        if(value && typeof value.current === "string" && typeof value.original === "string") fileCache[key]=value;
      }
    }
  } catch (_) { /* Storage can be unavailable in private browsing. */ }
  function persistDrafts() {
    try {
      if(typeof sessionStorage !== "undefined") sessionStorage.setItem(storageKey,JSON.stringify(Object.fromEntries(getKeys().filter(isDirty).map(key=>[key,fileCache[key]]))));
    } catch (_) { /* Keep in-memory edits even when browser storage is full. */ }
  }

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
        revision: data.revision,
        original: data.original || "",
        current:
          data.current !== undefined ? data.current : data.original || "",
      };
      persistDrafts();
      return;
    }

    if (data.original !== undefined) existing.original = data.original;
    if (data.current !== undefined) existing.current = data.current;
    if (data.name !== undefined) existing.name = data.name;
    if (data.project !== undefined) existing.project = data.project;
    if (data.revision !== undefined) existing.revision = data.revision;
    persistDrafts();
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
    const removed = delete fileCache[key]; persistDrafts(); return removed;
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


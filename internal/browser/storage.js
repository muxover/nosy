(async function () {
  var CAP = { items: 50, keys: 25, stores: 20, dbs: 20, entries: 20, props: 20, value: 300 };
  var out = { origin: location.origin, local: [], session: [], idb: [], caches: [], sw: [] };
  function clip(s) { s = String(s); return s.length > CAP.value ? s.slice(0, CAP.value) : s; }
  function within(p, ms) {
    return Promise.race([p, new Promise(function (_, reject) { setTimeout(function () { out.cut = true; reject(new Error('timeout')); }, ms); })]);
  }

  function items(store) {
    var list = [];
    for (var i = 0; i < store.length && i < CAP.items; i++) {
      var k = store.key(i), v = store.getItem(k);
      list.push({ key: k, value: clip(v), length: v == null ? 0 : v.length });
    }
    return list;
  }
  try { out.local = items(localStorage); } catch (e) {}
  try { out.session = items(sessionStorage); } catch (e) {}

  // the string values of one IndexedDB record: the value itself, or the
  // strings among its own properties
  function strings(value, into) {
    if (typeof value === 'string') { into.push({ key: '', value: clip(value), length: value.length }); return; }
    if (value && typeof value === 'object') {
      var names = Object.keys(value).slice(0, CAP.props);
      names.forEach(function (n) {
        var v = value[n];
        if (typeof v === 'string') into.push({ key: n, value: clip(v), length: v.length });
      });
    }
  }
  function request(r) {
    return new Promise(function (resolve, reject) { r.onsuccess = function () { resolve(r.result); }; r.onerror = function () { reject(r.error); }; });
  }
  function openDB(name) {
    return new Promise(function (resolve, reject) {
      var r = indexedDB.open(name);
      // a database that is gone by now must not be created again
      r.onupgradeneeded = function () { r.transaction.abort(); };
      r.onsuccess = function () { resolve(r.result); };
      r.onerror = function () { reject(r.error); };
      r.onblocked = function () { reject(new Error('blocked')); };
    });
  }
  async function readDB(info) {
    var db = await within(openDB(info.name), 2000);
    var d = { name: info.name, version: db.version, stores: [] };
    try {
      var names = Array.prototype.slice.call(db.objectStoreNames, 0, CAP.stores);
      for (var i = 0; i < names.length; i++) {
        var tx = db.transaction(names[i], 'readonly');
        var os = tx.objectStore(names[i]);
        var s = { name: names[i], entries: await request(os.count()), keys: [], values: [] };
        var keys = await request(os.getAllKeys(null, CAP.keys));
        s.keys = keys.map(function (k) { return clip(typeof k === 'string' ? k : JSON.stringify(k)); });
        var rows = await request(os.getAll(null, CAP.entries));
        rows.forEach(function (row) { strings(row, s.values); });
        d.stores.push(s);
      }
    } finally { db.close(); }
    return d;
  }
  try {
    if (indexedDB && indexedDB.databases) {
      var dbs = (await within(indexedDB.databases(), 2000)).slice(0, CAP.dbs);
      for (var i = 0; i < dbs.length; i++) {
        try { out.idb.push(await readDB(dbs[i])); } catch (e) { out.idb.push({ name: dbs[i].name, version: dbs[i].version || 0, stores: [] }); }
      }
    }
  } catch (e) {}

  try { if (self.caches) out.caches = (await within(caches.keys(), 2000)).slice(0, CAP.items); } catch (e) {}
  try {
    if (navigator.serviceWorker) {
      var regs = await within(navigator.serviceWorker.getRegistrations(), 2000);
      out.sw = regs.map(function (r) {
        var w = r.active || r.waiting || r.installing;
        return { scope: r.scope, script: w ? w.scriptURL : '' };
      });
    }
  } catch (e) {}
  return JSON.stringify(out);
})()

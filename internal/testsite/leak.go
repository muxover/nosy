//go:build nosytest

package testsite

import (
	"fmt"
	"net/http"
	"strconv"
)

// probeJS reads everything a page can keep between visits, sets one of each,
// reloads, and shows what it saw before and after. A browser that shares nothing
// between scans shows empty values before, in every scan, and the values it set
// after, so the page is its own control.
const probeJS = `(async function () {
  var out = document.getElementById('out');
  async function look() {
    var o = {};
    o.cookie = document.cookie;
    o.local = Object.keys(localStorage).join(',');
    o.session = Object.keys(sessionStorage).join(',');
    o.idb = (await indexedDB.databases()).map(function (d) { return d.name; }).join(',');
    o.caches = (await caches.keys()).join(',');
    o.sw = (await navigator.serviceWorker.getRegistrations()).map(function (r) { return r.scope; }).join(',');
    o.controlled = navigator.serviceWorker.controller ? 'yes' : 'no';
    o.cachedfile = await (await fetch('/cached.txt')).text();
    return o;
  }
  var first = window.name ? JSON.parse(window.name) : null;
  var seen = await look();
  if (!first) {
    document.cookie = 'leak=1; path=/; max-age=3600';
    localStorage.setItem('leak', '1');
    sessionStorage.setItem('leak', '1');
    await new Promise(function (done) {
      var r = indexedDB.open('leakdb');
      r.onsuccess = function () { r.result.close(); done(); };
      r.onerror = done;
    });
    await (await caches.open('leakcache')).put('/x', new Response('x'));
    await navigator.serviceWorker.register('/sw.js');
    await navigator.serviceWorker.ready;
    window.name = JSON.stringify(seen);
    location.reload();
    return;
  }
  out.textContent = 'PROBE ' + JSON.stringify({ first: first, second: seen });
})();`

func (s *Site) leakPages() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"leak.test": func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/sw.js":
				w.Header().Set("Service-Worker-Allowed", "/")
				script(w, `self.addEventListener('install', function () { self.skipWaiting(); });
self.addEventListener('activate', function (e) { e.waitUntil(clients.claim()); });`)
			case "/cached.txt":
				// the browser may keep this for an hour; the number says how often it was asked for
				s.mu.Lock()
				s.counters["leak.cached"]++
				n := s.counters["leak.cached"]
				s.mu.Unlock()
				w.Header().Set("Cache-Control", "max-age=3600")
				w.Header().Set("Content-Type", "text/plain")
				fmt.Fprint(w, strconv.Itoa(n))
			default:
				html(w, 200, page("Leak probe", `<pre id="out">PENDING</pre><script>`+probeJS+`</script>`))
			}
		},
	}
}

const stateJS = `(function () {
  var out = document.getElementById('out');
  function show() {
    out.textContent = 'STATE ' + JSON.stringify({ visible: document.visibilityState, focused: document.hasFocus(), width: innerWidth, height: innerHeight });
  }
  show();
  setTimeout(show, 1500);
  document.addEventListener('visibilitychange', show);
})();`

// A page that says whether it is shown and focused, as a visitor's would be.
func (s *Site) statePages() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"state.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("State probe", `<pre id="out">PENDING</pre><script>`+stateJS+`</script>`))
		},
	}
}

// Count is a named counter some test pages keep.
func (s *Site) Count(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counters[name]
}

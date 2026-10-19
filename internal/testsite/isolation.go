//go:build nosytest

package testsite

import (
	"fmt"
	"net/http"
)

// sandboxed is a frame that reports the origin it runs with, and tries to keep a
// value in local storage.
func sandboxed(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/origin" {
			noContent(w, r)
			return
		}
		html(w, 200, page("A sandboxed frame", body+`<script>
var stored = 'no';
try { localStorage.setItem('sandboxed', 'yes'); stored = 'yes'; } catch (e) {}
fetch('/origin?o=' + encodeURIComponent(self.origin) + '&stored=' + stored, {mode: 'no-cors'});
</script>`))
	}
}

// frames of other sites: with site isolation on, each runs in a process of its own
func frame(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { html(w, 200, page("A frame of another site", body)) }
}

const permsJS = `(async function () {
  var out = {};
  function tag(p) { return p.then(function () { return 'granted'; }, function (e) { return e && e.name || 'error'; }); }
  out.notification = await Notification.requestPermission();
  out.geolocation = await new Promise(function (done) {
    navigator.geolocation.getCurrentPosition(function () { done('granted'); }, function (e) { done('error' + e.code); }, { timeout: 4000 });
  });
  out.camera = await tag(navigator.mediaDevices.getUserMedia({ video: true }));
  out.microphone = await tag(navigator.mediaDevices.getUserMedia({ audio: true }));
  out.clipboard = await tag(navigator.clipboard.readText());
  out.midi = await tag(navigator.requestMIDIAccess({ sysex: true }));
  for (var name of ['geolocation', 'notifications', 'camera', 'microphone', 'midi', 'clipboard-read']) {
    out['query_' + name] = await navigator.permissions.query({ name: name }).then(function (s) { return s.state; }, function () { return 'unsupported'; });
  }
  await fetch('/report?' + new URLSearchParams(out), { mode: 'no-cors' });
})();`

func (s *Site) isolationPages() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		// one cross-site frame for every detector, a frame inside a frame, and a sandboxed one
		"xframes.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("Cross-site frames plant",
				`<iframe src="http://xfp.test/" width="300" height="80"></iframe>`+
					`<iframe src="https://xtrack.test/" width="300" height="80"></iframe>`+
					`<iframe src="http://xrec.test/" width="300" height="80"></iframe>`+
					`<iframe src="http://xkeys.test/" width="300" height="120"></iframe>`+
					`<iframe src="http://xnest.test/" width="300" height="120"></iframe>`+
					`<iframe sandbox="allow-scripts" src="http://xsandbox.test/" width="300" height="80"></iframe>`+
					`<iframe sandbox="allow-scripts allow-same-origin" src="http://xsandboxso.test/" width="300" height="80"></iframe>`+
					`<iframe src="https://xstore.test/" width="300" height="80"></iframe>`))
		},
		"xfp.test": frame(scriptTag("http://fpvendor.test/canvas.js") + `<script>` + canvasJS + `</script>`),
		"xtrack.test": func(w http.ResponseWriter, r *http.Request) {
			http.SetCookie(w, &http.Cookie{Name: "xuid", Value: "9f86d081884c7d659a2feaa0c55ad015", Path: "/", MaxAge: 31536000, SameSite: http.SameSiteNoneMode, Secure: true})
			html(w, 200, page("A tracking frame", `<img alt="" src="http://ads.test/px.gif">`))
		},
		"xrec.test":    frame(scriptTag("http://replayvendor.test/recorder.js")),
		"xkeys.test":   frame(`<form><label>Name <input type="text" name="q"></label></form>` + scriptTag("http://keylog.test/input-b64.js")),
		"xnest.test":   frame(`<iframe src="http://xnested.test/" width="260" height="80"></iframe>`),
		"xnested.test": frame(scriptTag("http://fpvendor.test/audio.js")),
		// sandboxed without allow-same-origin: its origin is opaque, and it says so
		"xsandbox.test": sandboxed(scriptTag("http://fpvendor.test/webgl.js")),
		// sandboxed with allow-same-origin: it keeps its origin and its storage
		"xsandboxso.test": sandboxed(scriptTag("http://fpvendor.test/fonts.js")),
		"xstore.test":     frame(`<script>` + tpStoreJS + `</script>`),

		// the same kinds of frames doing nothing wrong
		"xcontrol.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("Cross-site frames control", `<iframe src="http://xbenign.test/" width="300" height="120"></iframe>`))
		},
		"xbenign.test": frame(`<form><label>Search <input type="search" name="q"></label></form><script>` + controlJS + `</script>`),

		// the storage plant again; test builds give this host's storage read almost no time
		"cutshort.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("Storage read cut short", `<script>`+storeJS+`</script>`))
		},

		// asks for a new window when it loads and again with every key typed
		"popup.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("Pop-up plant", `<form><input type="text" name="q"></form><script>window.open('http://popped.test/load');
document.addEventListener('keydown', function () { window.open('http://popped.test/key'); }, true);</script>`))
		},
		"popped.test": frame(""),

		// starts a download when it loads
		"download.test": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/file.bin" {
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Content-Disposition", `attachment; filename="planted.bin"`)
				fmt.Fprint(w, "planted download body")
				return
			}
			html(w, 200, page("Download plant", `<a id="d" href="/file.bin" download>file</a><script>document.getElementById('d').click();
document.addEventListener('keydown', function () { document.getElementById('d').click(); }, true);</script><form><input type="text" name="q"></form>`))
		},

		// asks for every permission and reports what it got
		"perms.test": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/report" {
				noContent(w, r)
				return
			}
			html(w, 200, page("Permissions plant", `<script>`+permsJS+`</script>`))
		},

		// reaches for addresses on this machine
		"localnav.test": func(w http.ResponseWriter, r *http.Request) {
			local := s.LocalFile()
			html(w, 200, page("Local address plant", `<iframe src="`+local+`" width="200" height="40"></iframe><script>
window.open('chrome://settings');
setTimeout(function () { location.href = '`+local+`'; }, 1000);</script>`))
		},
		"localredir.test": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, s.LocalFile(), http.StatusFound)
		},

		// sets HSTS over TLS; the same host over plain HTTP shows whether the browser remembered
		"hsts.test": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/second" {
				html(w, 200, page("HSTS plant, second page", ""))
				return
			}
			if r.TLS != nil {
				w.Header().Set("Strict-Transport-Security", "max-age=3600")
			}
			html(w, 200, page("HSTS plant", `<script>setTimeout(function () { location.replace('http://hsts.test/second'); }, 600);</script>`))
		},
	}
}

// SetLocalFile gives the planted pages a file:// address to reach for.
func (s *Site) SetLocalFile(address string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.localFile = address
}

// LocalFile is that address.
func (s *Site) LocalFile() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.localFile
}

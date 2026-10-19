//go:build nosytest

package testsite

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// Vectors are the ways a page can make the browser send something, each on a
// planted page of its own: v<name>.test. Every one aims at the canaries.
var Vectors = []string{"fetch", "xhr", "ws", "img", "frame", "sw", "worker", "redir", "meta", "form", "rtc"}

// localTargets are the addresses on this machine and network a vector aims
// at: the canaries in several spellings, and private and metadata addresses
// that have no listener here.
func (s *Site) localTargets() []string {
	p := port(s.Canary.Addr)
	t := []string{
		"http://127.0.0.1:" + p + "/c",
		"http://localhost:" + p + "/c",
		"http://[::1]:" + p + "/c",
		"http://2130706433:" + p + "/c",
		"http://0x7f.1:" + p + "/c",
		"http://127.1:" + p + "/c",
		"http://[::ffff:7f00:1]:" + p + "/c",
		"https://127.0.0.1:" + p + "/c",
		"http://10.0.0.5/c",
		"http://192.168.1.1/c",
		"http://169.254.169.254/latest/meta-data/",
		"http://printer.local/c",
	}
	for _, c := range []*Canary{s.CanaryPort, s.HopCanary, s.LocalCanary} {
		if c != nil {
			t = append(t, "http://"+c.Addr+"/c")
		}
	}
	return t
}

func jsList(list []string) string {
	q := make([]string, len(list))
	for i, s := range list {
		q[i] = fmt.Sprintf("%q", s)
	}
	return "[" + strings.Join(q, ",") + "]"
}

// rtcJS connects two peers in the page and hands them candidates and servers
// on the canaries' UDP ports, so a browser that sends UDP itself would reach them.
func (s *Site) rtcJS() string {
	var stun []string
	var cands []string
	for _, c := range []*UDPCanary{s.UDPCanary, s.LocalUDPCanary} {
		if c == nil {
			continue
		}
		host, p, _ := net.SplitHostPort(c.Addr)
		stun = append(stun, fmt.Sprintf("{urls: 'stun:%s:%s'}", host, p))
		cands = append(cands, fmt.Sprintf("'candidate:1 1 udp 2122260223 %s %s typ host'", host, p))
	}
	tcp := port(s.Canary.Addr)
	stun = append(stun, fmt.Sprintf("{urls: 'turn:127.0.0.1:%s?transport=tcp', username: 'u', credential: 'p'}", tcp))
	return `(async function () {
  var servers = {iceServers: [` + strings.Join(stun, ", ") + `]};
  var a = new RTCPeerConnection(servers), b = new RTCPeerConnection(servers);
  a.onicecandidate = function (e) { if (e.candidate) b.addIceCandidate(e.candidate).catch(function () {}); };
  b.onicecandidate = function (e) { if (e.candidate) a.addIceCandidate(e.candidate).catch(function () {}); };
  a.createDataChannel('x');
  await a.setLocalDescription(await a.createOffer());
  await b.setRemoteDescription(a.localDescription);
  await b.setLocalDescription(await b.createAnswer());
  await a.setRemoteDescription(b.localDescription);
  [` + strings.Join(cands, ", ") + `].forEach(function (c) {
    a.addIceCandidate({candidate: c, sdpMid: '0', sdpMLineIndex: 0}).catch(function () {});
  });
})();`
}

func (s *Site) networkPages() map[string]http.HandlerFunc {
	targets := func() string { return jsList(s.localTargets()) }
	each := func(js string) string {
		return `<script>` + targets() + `.forEach(function (u) { try { ` + js + ` } catch (e) {} });</script>`
	}
	return map[string]http.HandlerFunc{
		"vfetch.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("Fetch to the local network", each(`fetch(u, {mode: 'no-cors'}).catch(function () {});`)))
		},
		"vxhr.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("XHR to the local network", each(`var x = new XMLHttpRequest(); x.open('GET', u); x.send();`)))
		},
		"vws.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("WebSocket to the local network", each(`new WebSocket(u.replace(/^http/, 'ws'));`)))
		},
		"vimg.test": func(w http.ResponseWriter, r *http.Request) {
			var b strings.Builder
			for _, u := range s.localTargets() {
				fmt.Fprintf(&b, `<img alt="" src="%s">`, u)
			}
			html(w, 200, page("Images from the local network", b.String()))
		},
		"vframe.test": func(w http.ResponseWriter, r *http.Request) {
			var b strings.Builder
			for _, u := range s.localTargets() {
				fmt.Fprintf(&b, `<iframe src="%s" width="10" height="10"></iframe>`, u)
			}
			html(w, 200, page("Frames from the local network", b.String()))
		},
		"vsw.test": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/sw.js" {
				w.Header().Set("Service-Worker-Allowed", "/")
				script(w, `self.addEventListener('activate', function (e) { e.waitUntil(Promise.all(`+targets()+`.map(function (u) {
  return fetch(u, {mode: 'no-cors'}).catch(function () {});
}))); });`)
				return
			}
			html(w, 200, page("Service worker to the local network", `<script>navigator.serviceWorker.register('/sw.js');</script>`))
		},
		"vworker.test": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/w.js" {
				script(w, targets()+`.forEach(function (u) { fetch(u, {mode: 'no-cors'}).catch(function () {}); });`)
				return
			}
			html(w, 200, page("Worker to the local network", `<script>new Worker('/w.js');</script>`))
		},
		// public to private on the second hop and on the third
		"vredir.test": func(w http.ResponseWriter, r *http.Request) {
			targets := s.localTargets()
			switch {
			case r.URL.Path == "/":
				var b strings.Builder
				for i := range targets {
					fmt.Fprintf(&b, `<img alt="" src="/two/%d"><img alt="" src="/three/%d">`, i, i)
				}
				html(w, 200, page("Redirects to the local network", b.String()))
			case strings.HasPrefix(r.URL.Path, "/two/"):
				http.Redirect(w, r, targets[index(r.URL.Path, len(targets))], http.StatusFound)
			case strings.HasPrefix(r.URL.Path, "/three/"):
				http.Redirect(w, r, "http://vredir2.test/hop/"+strings.TrimPrefix(r.URL.Path, "/three/"), http.StatusFound)
			}
		},
		"vredir2.test": func(w http.ResponseWriter, r *http.Request) {
			targets := s.localTargets()
			http.Redirect(w, r, targets[index(r.URL.Path, len(targets))], http.StatusFound)
		},
		"vmeta.test": func(w http.ResponseWriter, r *http.Request) {
			targets := s.localTargets()
			if strings.HasPrefix(r.URL.Path, "/frame/") {
				html(w, 200, `<!doctype html><meta http-equiv="refresh" content="0;url=`+targets[index(r.URL.Path, len(targets))]+`"><title>.</title>`)
				return
			}
			var b strings.Builder
			for i := range targets {
				fmt.Fprintf(&b, `<iframe src="/frame/%d" width="10" height="10"></iframe>`, i)
			}
			html(w, 200, page("Meta refresh to the local network", b.String()))
		},
		"vform.test": func(w http.ResponseWriter, r *http.Request) {
			var b strings.Builder
			for i, u := range s.localTargets() {
				fmt.Fprintf(&b, `<iframe name="f%d" width="10" height="10"></iframe><form id="form%d" action="%s" method="get" target="f%d"><input name="q" value="x"></form>`, i, i, u, i)
			}
			b.WriteString(`<script>document.querySelectorAll('form').forEach(function (f) { f.submit(); });</script>`)
			html(w, 200, page("Forms sent to the local network", b.String()))
		},
		"vrtc.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("WebRTC to the local network", `<script>`+s.rtcJS()+`</script>`))
		},
	}
}

// index reads the number at the end of path, within n.
func index(path string, n int) int {
	k, err := strconv.Atoi(path[strings.LastIndexByte(path, '/')+1:])
	if err != nil || k < 0 || k >= n {
		return 0
	}
	return k
}

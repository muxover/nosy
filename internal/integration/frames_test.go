//go:build nosytest

package integration

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/muxover/nosy/internal/scan"
)

// scanFrames scans the cross-site frames plant once and returns the result and
// the browser's own log, which says which frames ran in a process of their own.
func scanFrames(t *testing.T) (*scan.Result, []string) {
	t.Helper()
	var mu sync.Mutex
	var log []string
	r := scanTest(t, "http://xframes.test/", func(o *scan.Options) {
		o.Verbose = func(format string, args ...any) {
			line := strings.TrimSpace(strings.ReplaceAll(fmt.Sprintf(format, args...), "\n", " "))
			mu.Lock()
			log = append(log, line)
			mu.Unlock()
			t.Log(line)
		}
	})
	mu.Lock()
	defer mu.Unlock()
	return r, append([]string(nil), log...)
}

func TestCrossSiteFramesAreWatched(t *testing.T) {
	t.Parallel()
	r, log := scanFrames(t)
	if r.Status != scan.StatusDone {
		t.Fatalf("status %s: %s", r.Status, r.Reason)
	}

	t.Run("frames run in processes of their own", func(t *testing.T) {
		attached := 0
		for _, l := range log {
			if strings.Contains(l, "attached iframe") {
				attached++
			}
		}
		// five frames of other sites, one inside one of them, and the two sandboxed ones
		if attached < 8 {
			t.Errorf("%d frames were attached as their own targets, want at least 8: site isolation is not on\n%s", attached, strings.Join(log, "\n"))
		}
	})

	t.Run("fingerprinting is attributed to the frame", func(t *testing.T) {
		var canvas, audio, webgl []string
		var canvasFrames, audioFrames []string
		for _, f := range r.Fingerprinting {
			switch f.Name {
			case "canvas":
				canvas, canvasFrames = f.Scripts, f.Frames
			case "audio":
				audio, audioFrames = f.Scripts, f.Frames
			case "webgl":
				webgl = f.Scripts
			}
		}
		if !has(canvas, "http://fpvendor.test/canvas.js") || !has(canvas, "http://xfp.test/") || !has(canvasFrames, "http://xfp.test") {
			t.Errorf("canvas scripts %v frames %v, want the vendor script and the frame's own, in http://xfp.test", canvas, canvasFrames)
		}
		if !has(audio, "http://fpvendor.test/audio.js") || !has(audioFrames, "http://xnested.test") {
			t.Errorf("audio from the frame inside a frame: scripts %v frames %v", audio, audioFrames)
		}
		if !has(webgl, "http://fpvendor.test/webgl.js") {
			t.Errorf("webgl from the sandboxed frame: scripts %v", webgl)
		}
	})

	t.Run("requests carry the frame that made them", func(t *testing.T) {
		found := false
		for _, q := range r.Requests {
			if q.Host == "ads.test" {
				found = q.Frame == "https://xtrack.test"
			}
		}
		if !found {
			t.Errorf("the pixel from https://xtrack.test was not seen as that frame's request")
		}
		if found, tracking := company(r, "AdNet"); !found || !tracking {
			t.Errorf("AdNet not seen as tracking: %+v", r.Companies)
		}
	})

	t.Run("cookies set by a frame are third-party", func(t *testing.T) {
		for _, c := range r.Cookies {
			if c.Name == "xuid" {
				if !c.ThirdParty || !c.Identifier || c.Domain != "xtrack.test" {
					t.Errorf("cookie = %+v", c)
				}
				return
			}
		}
		t.Errorf("the cookie from the frame is missing: %+v", r.Cookies)
	})

	t.Run("session recording in a frame", func(t *testing.T) {
		for _, e := range r.SessionRecording {
			if e.Frame == "http://xrec.test" {
				return
			}
		}
		t.Errorf("recording = %+v, want one from http://xrec.test", r.SessionRecording)
	})

	t.Run("typed text sent from a frame", func(t *testing.T) {
		if r.TypedText.FieldsTested == 0 {
			t.Fatalf("no field was typed into: %+v", r.TypedText)
		}
		for _, h := range r.TypedText.Hits {
			if h.Frame == "http://xkeys.test" && h.ThirdParty {
				return
			}
		}
		t.Errorf("hits = %+v, want one sent from http://xkeys.test to a third party", r.TypedText.Hits)
	})

	t.Run("sandboxed frames", func(t *testing.T) {
		said := map[string]string{}
		for _, host := range []string{"xsandbox.test", "xsandboxso.test"} {
			for _, q := range fixture.Requests(host) {
				if q.Path == "/origin" {
					said[host] = q.Query
				}
			}
		}
		// without allow-same-origin the frame's origin is opaque and it keeps nothing
		if said["xsandbox.test"] != "o=null&stored=no" {
			t.Errorf("the sandboxed frame said %q, want an opaque origin and no storage", said["xsandbox.test"])
		}
		if said["xsandboxso.test"] != "o=http%3A%2F%2Fxsandboxso.test&stored=yes" {
			t.Errorf("the frame with allow-same-origin said %q, want its own origin and storage", said["xsandboxso.test"])
		}
		// both are named by their address
		var webgl, fonts []string
		for _, f := range r.Fingerprinting {
			switch f.Name {
			case "webgl":
				webgl = f.Frames
			case "fonts":
				fonts = f.Frames
			}
		}
		if !has(webgl, "http://xsandbox.test") || !has(fonts, "http://xsandboxso.test") {
			t.Errorf("webgl frames %v, fonts frames %v", webgl, fonts)
		}
		kept := map[string]bool{}
		for _, o := range r.Storage {
			kept[o.Origin] = true
		}
		if !kept["http://xsandboxso.test"] || kept["http://xsandbox.test"] {
			t.Errorf("storage origins %v, want the frame with allow-same-origin only", kept)
		}
	})

	t.Run("storage of a frame stands apart", func(t *testing.T) {
		for _, o := range r.Storage {
			if o.Origin == "https://xstore.test" {
				if !o.ThirdParty || len(o.LocalStorage) == 0 || len(o.IndexedDB) == 0 {
					t.Errorf("storage = %+v", o)
				}
				return
			}
		}
		t.Errorf("no storage from https://xstore.test: %+v", r.Storage)
	})
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestFramesDoingNothingWrongStayQuiet(t *testing.T) {
	t.Parallel()
	r := scanTest(t, "http://xcontrol.test/")
	if len(r.Fingerprinting) != 0 || len(r.SessionRecording) != 0 || len(r.TypedText.Hits) != 0 {
		t.Errorf("a quiet frame was flagged: fingerprinting %+v recording %+v keys %+v", r.Fingerprinting, r.SessionRecording, r.TypedText.Hits)
	}
	if r.TypedText.FieldsTested == 0 {
		t.Errorf("the field in the frame was not typed into, so the control proves nothing")
	}
	if r.Score == nil || r.Score.Total != 0 {
		t.Errorf("score = %+v, want 0", r.Score)
	}
}

func TestConditionsRecordIsolationAndWebGL(t *testing.T) {
	t.Parallel()
	r := scanTest(t, "http://control.test/")
	c := r.Conditions
	if !has(c.Settings, "site isolation on") {
		t.Errorf("settings = %v, want site isolation on", c.Settings)
	}
	if c.WebGL != "hardware" && c.WebGL != "software" {
		t.Errorf("webgl = %q, want hardware or software on a machine that can draw", c.WebGL)
	}
	if (c.WebGL == "software") != has(c.Settings, "software WebGL (no GPU found)") {
		t.Errorf("webgl %q does not match the settings %v", c.WebGL, c.Settings)
	}
	// a slow machine can cut even this read short, but never without saying so
	if c.StorageCutShort && !hasNote(r, "storage read cut short") {
		t.Errorf("the storage read was cut short and no note says so: %v", r.Notes)
	}
}

func TestStorageReadCutShortIsSaid(t *testing.T) {
	t.Parallel()
	r := scanTest(t, "https://cutshort.test/")
	if !r.Conditions.StorageCutShort {
		t.Errorf("conditions = %+v, want the storage read cut short", r.Conditions)
	}
	if !hasNote(r, "storage read cut short") {
		t.Errorf("notes = %v, want one that says the storage read was cut short", r.Notes)
	}
}

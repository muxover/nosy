package chrome

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

type entry struct {
	name string
	body string
	mode os.FileMode
}

func makeZip(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		h.SetMode(e.mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(e.body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

var goodEntries = []entry{
	{"chrome-linux64/", "", os.ModeDir | 0o755},
	{"chrome-linux64/chrome", "#!/bin/sh\necho Chrome/155.0.0.0\n", 0o755},
	{"chrome-linux64/lib/libfoo.so", "lib", 0o644},
	{"chrome-linux64/libfoo.link", "lib/libfoo.so", os.ModeSymlink | 0o777},
}

// server serves body at any path and counts the requests.
func server(t *testing.T, body []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func installerFor(t *testing.T, srv *httptest.Server, zipBytes []byte) *Installer {
	t.Helper()
	sum := sha256.Sum256(zipBytes)
	u, _ := url.Parse(srv.URL)
	return &Installer{
		Version:     "155.0.0.1",
		Platform:    Platform{Name: "linux64", Size: int64(len(zipBytes)), SHA256: hex.EncodeToString(sum[:]), Exec: "chrome-linux64/chrome"},
		Dir:         filepath.Join(t.TempDir(), "nosy", "chrome"),
		BaseURL:     srv.URL,
		AllowedHost: u.Host,
		Client:      srv.Client(),
	}
}

func TestInstallDownloadsChecksAndUnpacks(t *testing.T) {
	z := makeZip(t, goodEntries)
	srv, hits := server(t, z)
	in := installerFor(t, srv, z)
	var last struct {
		done, total int64
		phases      map[string]bool
	}
	last.phases = map[string]bool{}
	in.Progress = func(done, total int64, phase string) {
		last.phases[phase] = true
		if phase == "downloading" {
			last.done, last.total = done, total
		}
	}
	if _, ok := in.Installed(); ok {
		t.Fatal("installed before installing")
	}
	path, err := in.Install(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(in.Dir, "155.0.0.1", "chrome-linux64", "chrome"); path != want {
		t.Errorf("path = %s, want %s", path, want)
	}
	if got, ok := in.Installed(); !ok || got != path {
		t.Errorf("Installed = %s, %v", got, ok)
	}
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(path); err != nil || fi.Mode().Perm()&0o100 == 0 {
			t.Errorf("the browser is not executable: %v %v", fi, err)
		}
		link, err := os.Readlink(filepath.Join(in.Dir, "155.0.0.1", "chrome-linux64", "libfoo.link"))
		if err != nil || link != "lib/libfoo.so" {
			t.Errorf("symlink = %q, %v", link, err)
		}
	}
	if last.done != int64(len(z)) || last.total != int64(len(z)) || !last.phases["downloading"] || !last.phases["verifying"] || !last.phases["unpacking"] {
		t.Errorf("progress = %+v", last)
	}
	// nothing but the version folder is left in the cache
	entries, _ := os.ReadDir(in.Dir)
	if len(entries) != 1 || entries[0].Name() != "155.0.0.1" {
		t.Errorf("cache holds %v", entries)
	}

	// a second install keeps the first, and --force downloads again
	if _, err := in.Install(context.Background(), false); err != nil || hits.Load() != 1 {
		t.Errorf("second install: %v, %d downloads", err, hits.Load())
	}
	if _, err := in.Install(context.Background(), true); err != nil || hits.Load() != 2 {
		t.Errorf("forced install: %v, %d downloads", err, hits.Load())
	}
	if _, ok := in.Installed(); !ok {
		t.Error("not installed after a forced install")
	}
}

func TestInstallRefusesADownloadThatDiffersFromThePin(t *testing.T) {
	z := makeZip(t, goodEntries)
	// the same length, different bytes
	tampered := append([]byte(nil), z...)
	tampered[len(tampered)/2] ^= 0xff
	srv, _ := server(t, tampered)
	in := installerFor(t, srv, z)
	_, err := in.Install(context.Background(), false)
	if !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("error = %v, want a hash mismatch", err)
	}
	if _, ok := in.Installed(); ok {
		t.Error("a tampered download was installed")
	}
	if left, _ := os.ReadDir(in.Dir); len(left) != 0 {
		t.Errorf("a tampered download left %v behind", left)
	}
}

func TestInstallRefusesAWrongSize(t *testing.T) {
	z := makeZip(t, goodEntries)
	srv, _ := server(t, append(append([]byte(nil), z...), "extra"...))
	in := installerFor(t, srv, z)
	if _, err := in.Install(context.Background(), false); !errors.Is(err, ErrHashMismatch) {
		t.Errorf("a longer download: %v", err)
	}
	srv2, _ := server(t, z[:len(z)-10])
	in2 := installerFor(t, srv2, z)
	if _, err := in2.Install(context.Background(), false); err == nil {
		t.Error("a shorter download was accepted")
	}
}

func TestInstallFollowsNoRedirectToAnotherHost(t *testing.T) {
	z := makeZip(t, goodEntries)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/chrome.zip", http.StatusFound)
	}))
	defer srv.Close()
	in := installerFor(t, srv, z)
	_, err := in.Install(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "refusing a redirect to evil.example") {
		t.Errorf("error = %v", err)
	}
}

func TestInstallOnlyFromHTTPSOnTheTrustedHost(t *testing.T) {
	z := makeZip(t, goodEntries)
	srv, _ := server(t, z)
	in := installerFor(t, srv, z)
	in.BaseURL = strings.Replace(srv.URL, "https://", "http://", 1)
	if _, err := in.Install(context.Background(), false); err == nil {
		t.Error("a plain HTTP download was allowed")
	}
	in = installerFor(t, srv, z)
	in.AllowedHost = "storage.googleapis.com"
	if _, err := in.Install(context.Background(), false); err == nil {
		t.Error("a download from another host was allowed")
	}
	// the default is Google's official host and nothing else
	d := &Installer{Version: "1.2.3.4", Platform: Platform{Name: "linux64"}}
	if got := d.zipURL(); got != "https://storage.googleapis.com/chrome-for-testing-public/1.2.3.4/linux64/chrome-linux64.zip" {
		t.Errorf("default URL = %s", got)
	}
}

func TestInstallRefusesPathsAndLinksOutOfTheFolder(t *testing.T) {
	bad := map[string][]entry{
		"dot dot":       {{"../evil", "x", 0o644}},
		"deep dot dot":  {{"chrome-linux64/../../evil", "x", 0o644}},
		"absolute link": {{"chrome-linux64/chrome", "x", 0o755}, {"chrome-linux64/l", "/etc/passwd", os.ModeSymlink | 0o777}},
		"escaping link": {{"chrome-linux64/chrome", "x", 0o755}, {"chrome-linux64/l", "../../outside", os.ModeSymlink | 0o777}},
	}
	for name, entries := range bad {
		if runtime.GOOS == "windows" && strings.Contains(name, "link") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			z := makeZip(t, entries)
			srv, _ := server(t, z)
			in := installerFor(t, srv, z)
			if _, err := in.Install(context.Background(), false); err == nil {
				t.Fatal("installed")
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(in.Dir), "evil")); err == nil {
				t.Error("a file was written outside the folder")
			}
			if left, _ := os.ReadDir(in.Dir); len(left) != 0 {
				t.Errorf("left behind: %v", left)
			}
		})
	}
}

func TestInstallNeedsTheBrowserInTheArchive(t *testing.T) {
	z := makeZip(t, []entry{{"chrome-linux64/other", "x", 0o755}})
	srv, _ := server(t, z)
	in := installerFor(t, srv, z)
	if _, err := in.Install(context.Background(), false); err == nil || !strings.Contains(err.Error(), "no browser") {
		t.Errorf("error = %v", err)
	}
}

func TestRemoveDeletesOnlyNosysOwnFolder(t *testing.T) {
	z := makeZip(t, goodEntries)
	srv, _ := server(t, z)
	in := installerFor(t, srv, z)
	if removed, err := in.Remove(); err != nil || removed {
		t.Errorf("nothing installed: %v, %v", removed, err)
	}
	if _, err := in.Install(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(filepath.Dir(in.Dir), "keep.txt")
	os.WriteFile(sibling, []byte("x"), 0o644)
	if removed, err := in.Remove(); err != nil || !removed {
		t.Fatalf("remove: %v, %v", removed, err)
	}
	if _, err := os.Stat(in.Dir); err == nil {
		t.Error("the browser folder is still there")
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Error("something next to the browser folder was deleted")
	}
	// a folder that is not Nosy's own is never deleted
	other := t.TempDir()
	os.WriteFile(filepath.Join(other, "file"), []byte("x"), 0o644)
	odd := &Installer{Dir: other}
	if _, err := odd.Remove(); err == nil {
		t.Error("removed a folder that is not Nosy's")
	}
	if _, err := os.Stat(filepath.Join(other, "file")); err != nil {
		t.Error("the file in another folder is gone")
	}
}

func TestPlatforms(t *testing.T) {
	for _, c := range []struct{ goos, goarch, name string }{
		{"linux", "amd64", "linux64"}, {"darwin", "arm64", "mac-arm64"}, {"darwin", "amd64", "mac-x64"}, {"windows", "amd64", "win64"},
	} {
		p, err := platformFor(c.goos, c.goarch)
		if err != nil || p.Name != c.name || p.Exec == "" || len(p.SHA256) != 64 || p.Size <= 0 {
			t.Errorf("%s/%s = %+v, %v", c.goos, c.goarch, p, err)
		}
	}
	for _, c := range [][2]string{{"linux", "arm64"}, {"windows", "arm64"}, {"freebsd", "amd64"}, {"linux", "386"}} {
		_, err := platformFor(c[0], c[1])
		if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "NOSY_CHROME") {
			t.Errorf("%v = %v", c, err)
		}
	}
	// every pin is a lower-case SHA-256 and the version is a Chrome version
	for name, p := range pins {
		if len(p.SHA256) != 64 || strings.ToLower(p.SHA256) != p.SHA256 {
			t.Errorf("%s has the pin %q", name, p.SHA256)
		}
	}
	if n := strings.Count(Version, "."); n != 3 {
		t.Errorf("version %q", Version)
	}
}

package chrome

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// officialHost serves the Chrome for Testing downloads. Nothing else is
// downloaded from, and a redirect to any other host is refused.
const officialHost = "storage.googleapis.com"

const officialBase = "https://" + officialHost + "/chrome-for-testing-public"

// ErrUnsupported means there is no pinned build for this system.
var ErrUnsupported = errors.New("no pinned Chrome build for this system")

// ErrHashMismatch means a download did not match the SHA-256 pinned in the code.
var ErrHashMismatch = errors.New("the download does not match the pinned SHA-256")

// Platform is one Chrome for Testing build Nosy can install.
type Platform struct {
	Name   string // linux64, mac-arm64, mac-x64, win64
	Size   int64  // bytes of the zip
	SHA256 string // of the zip
	// Exec is the browser inside the unpacked archive.
	Exec string
}

// CurrentPlatform is the pinned build for the system Nosy runs on.
func CurrentPlatform() (Platform, error) {
	return platformFor(runtime.GOOS, runtime.GOARCH)
}

func platformFor(goos, goarch string) (Platform, error) {
	var name string
	switch {
	case goos == "linux" && goarch == "amd64":
		name = "linux64"
	case goos == "darwin" && goarch == "arm64":
		name = "mac-arm64"
	case goos == "darwin" && goarch == "amd64":
		name = "mac-x64"
	case goos == "windows" && goarch == "amd64":
		name = "win64"
	default:
		return Platform{}, fmt.Errorf("%w (%s/%s): install Chrome or Chromium from your system's packages and Nosy will find it, or set NOSY_CHROME", ErrUnsupported, goos, goarch)
	}
	p, ok := pins[name]
	if !ok {
		return Platform{}, fmt.Errorf("%w (%s)", ErrUnsupported, name)
	}
	p.Name = name
	switch name {
	case "linux64":
		p.Exec = "chrome-linux64/chrome"
	case "mac-arm64", "mac-x64":
		p.Exec = "chrome-" + name + "/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing"
	case "win64":
		p.Exec = `chrome-win64\chrome.exe`
	}
	return p, nil
}

// Installer downloads and keeps Nosy's pinned Chrome.
type Installer struct {
	Version  string
	Platform Platform
	// Dir holds one folder per installed version.
	Dir string

	// BaseURL, AllowedHost and Client are replaced in tests; the defaults
	// are Google's official host.
	BaseURL     string
	AllowedHost string
	Client      *http.Client

	// Progress is told how much of the download is done, and the phase.
	Progress func(done, total int64, phase string)
}

// DefaultInstaller installs into the user's cache directory.
func DefaultInstaller(cacheDir func() (string, error)) (*Installer, error) {
	p, err := CurrentPlatform()
	if err != nil {
		return nil, err
	}
	dir, err := cacheDir()
	if err != nil {
		return nil, fmt.Errorf("no cache directory on this system: %w", err)
	}
	return &Installer{Version: Version, Platform: p, Dir: filepath.Join(dir, "nosy", "chrome")}, nil
}

// Root is the folder of Nosy's downloaded browsers.
func (i *Installer) Root() string { return i.Dir }

func (i *Installer) versionDir() string { return filepath.Join(i.Dir, i.Version) }

// Installed returns the pinned browser's path when it is installed.
func (i *Installer) Installed() (string, bool) {
	path := filepath.Join(i.versionDir(), filepath.FromSlash(i.Platform.Exec))
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		if _, err := os.Stat(filepath.Join(i.versionDir(), markerName)); err == nil {
			return path, true
		}
	}
	return "", false
}

const markerName = ".nosy-install.json"

type marker struct {
	Version   string `json:"version"`
	Platform  string `json:"platform"`
	SHA256    string `json:"sha256"`
	Installed string `json:"installed"`
}

func (i *Installer) host() string {
	if i.AllowedHost != "" {
		return i.AllowedHost
	}
	return officialHost
}

func (i *Installer) zipURL() string {
	base := i.BaseURL
	if base == "" {
		base = officialBase
	}
	return fmt.Sprintf("%s/%s/%s/chrome-%s.zip", base, i.Version, i.Platform.Name, i.Platform.Name)
}

func (i *Installer) client() *http.Client {
	c := i.Client
	if c == nil {
		c = &http.Client{}
	}
	c2 := *c
	c2.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || req.URL.Host != i.host() {
			return fmt.Errorf("refusing a redirect to %s: only %s is trusted", req.URL.Host, i.host())
		}
		if len(via) > 5 {
			return errors.New("too many redirects")
		}
		return nil
	}
	return &c2
}

func (i *Installer) report(done, total int64, phase string) {
	if i.Progress != nil {
		i.Progress(done, total, phase)
	}
}

// Install downloads the pinned build, checks its SHA-256, unpacks it and
// returns the path of the browser. An existing install is kept unless force.
func (i *Installer) Install(ctx context.Context, force bool) (string, error) {
	if path, ok := i.Installed(); ok && !force {
		return path, nil
	}
	u, err := url.Parse(i.zipURL())
	if err != nil || u.Scheme != "https" || u.Host != i.host() {
		return "", fmt.Errorf("the download address %q is not on %s", i.zipURL(), i.host())
	}
	if err := os.MkdirAll(i.Dir, 0o755); err != nil {
		return "", err
	}
	zipFile, err := os.CreateTemp(i.Dir, "download-*.zip")
	if err != nil {
		return "", err
	}
	defer os.Remove(zipFile.Name())
	defer zipFile.Close()

	if err := i.download(ctx, u.String(), zipFile); err != nil {
		return "", err
	}
	i.report(0, 0, "unpacking")
	staging, err := os.MkdirTemp(i.Dir, i.Version+".partial-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	if err := unzip(zipFile.Name(), staging); err != nil {
		return "", fmt.Errorf("unpack the download: %w", err)
	}
	if _, err := os.Stat(filepath.Join(staging, filepath.FromSlash(i.Platform.Exec))); err != nil {
		return "", fmt.Errorf("the download has no browser at %s", i.Platform.Exec)
	}
	m, _ := json.Marshal(marker{Version: i.Version, Platform: i.Platform.Name, SHA256: i.Platform.SHA256, Installed: time.Now().UTC().Format(time.RFC3339)})
	if err := os.WriteFile(filepath.Join(staging, markerName), m, 0o644); err != nil {
		return "", err
	}
	if err := os.RemoveAll(i.versionDir()); err != nil {
		return "", err
	}
	if err := os.Rename(staging, i.versionDir()); err != nil {
		return "", err
	}
	path, ok := i.Installed()
	if !ok {
		return "", errors.New("the install is not where it was put")
	}
	return path, nil
}

// download streams the zip into f, counting its SHA-256 as it comes, and
// refuses it unless size and hash are the pinned ones.
func (i *Installer) download(ctx context.Context, rawURL string, f *os.File) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := i.client().Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", rawURL, resp.StatusCode)
	}
	total := resp.ContentLength
	if total < 0 {
		total = i.Platform.Size
	}
	if total != i.Platform.Size {
		return fmt.Errorf("%w: the server sent %d bytes and %d are pinned", ErrHashMismatch, total, i.Platform.Size)
	}
	h := sha256.New()
	// never read more than was pinned, so a wrong server cannot fill the disk
	r := io.LimitReader(resp.Body, i.Platform.Size+1)
	var done int64
	buf := make([]byte, 256<<10)
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			done += int64(n)
			i.report(done, total, "downloading")
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fmt.Errorf("download %s: %w", rawURL, rerr)
		}
	}
	if done != i.Platform.Size {
		return fmt.Errorf("%w: got %d bytes and %d are pinned", ErrHashMismatch, done, i.Platform.Size)
	}
	i.report(done, total, "verifying")
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, i.Platform.SHA256) {
		return fmt.Errorf("%w: got %s, pinned %s", ErrHashMismatch, got, i.Platform.SHA256)
	}
	return f.Sync()
}

// Remove deletes the folder of Nosy's downloaded browsers and nothing else.
// It reports whether anything was there.
func (i *Installer) Remove() (bool, error) {
	if _, err := os.Stat(i.Dir); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	// only ever the folder Nosy made: it holds version folders and our own markers
	if filepath.Base(i.Dir) != "chrome" || filepath.Base(filepath.Dir(i.Dir)) != "nosy" {
		return false, fmt.Errorf("refusing to delete %s: it is not Nosy's browser folder", i.Dir)
	}
	return true, os.RemoveAll(i.Dir)
}

// unzip unpacks src into dest. Names that leave dest, links that point out of
// it and archives that unpack to more than maxUnpacked are refused.
func unzip(src, dest string) error {
	const (
		maxFiles    = 50000
		maxUnpacked = 3 << 30
	)
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()
	if len(zr.File) > maxFiles {
		return fmt.Errorf("the archive has %d entries", len(zr.File))
	}
	root, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	var unpacked int64
	for _, f := range zr.File {
		name := filepath.FromSlash(f.Name)
		target := filepath.Join(root, name)
		if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return fmt.Errorf("the archive has a path outside the folder: %s", f.Name)
		}
		mode := f.Mode()
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case mode&os.ModeSymlink != 0:
			link, err := readAll(f, 4096)
			if err != nil {
				return err
			}
			if err := checkLink(root, target, string(link)); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(string(link), target); err != nil {
				return err
			}
		case mode.IsRegular():
			unpacked += int64(f.UncompressedSize64)
			if unpacked > maxUnpacked {
				return errors.New("the archive unpacks to more than 3 GB")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := writeEntry(f, target, mode.Perm()|0o200); err != nil {
				return err
			}
		}
	}
	return nil
}

func readAll(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, limit))
}

func writeEntry(f *zip.File, target string, perm os.FileMode) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(rc, int64(f.UncompressedSize64)+1)); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// checkLink refuses a symlink that is absolute or resolves outside root.
func checkLink(root, target, link string) error {
	if filepath.IsAbs(link) || strings.HasPrefix(link, "/") {
		return fmt.Errorf("the archive has an absolute link: %s", link)
	}
	resolved := filepath.Join(filepath.Dir(target), link)
	if resolved != root && !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
		return fmt.Errorf("the archive has a link out of the folder: %s", link)
	}
	return nil
}

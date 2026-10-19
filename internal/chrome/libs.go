package chrome

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// MissingLibs lists the shared libraries the browser at path needs and the
// system lacks, as ldd sees them. It is for Linux; elsewhere nothing is
// reported.
func MissingLibs(ctx context.Context, path string) ([]string, error) {
	return missingLibs(ctx, path, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	})
}

type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

func missingLibs(ctx context.Context, path string, run runner) ([]string, error) {
	out, err := run(ctx, "ldd", path)
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("could not run ldd: %w", err)
	}
	var missing []string
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if name, ok := strings.CutSuffix(line, "=> not found"); ok {
			missing = append(missing, strings.TrimSpace(name))
		}
	}
	sort.Strings(missing)
	return missing, nil
}

// Family is a group of Linux distributions that share a package manager.
type Family string

const (
	Debian Family = "debian" // Debian, Ubuntu and their derivatives
	Fedora Family = "fedora" // Fedora, RHEL and their derivatives
	Arch   Family = "arch"
	SUSE   Family = "suse"
)

// FamilyOf reads /etc/os-release (its text is passed in) and says which
// package manager the system uses. It returns false for a system it does not know.
func FamilyOf(osRelease string) (Family, bool) {
	var ids []string
	for _, line := range strings.Split(osRelease, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || (k != "ID" && k != "ID_LIKE") {
			continue
		}
		ids = append(ids, strings.Fields(strings.Trim(v, `"'`))...)
	}
	for _, id := range ids {
		switch id {
		case "debian", "ubuntu":
			return Debian, true
		case "fedora", "rhel", "centos", "rocky", "almalinux":
			return Fedora, true
		case "arch", "manjaro":
			return Arch, true
		case "suse", "opensuse", "opensuse-leap", "opensuse-tumbleweed", "sles":
			return SUSE, true
		}
	}
	return "", false
}

// the packages that hold the libraries a Chrome build links to, by family
var libPackages = map[string]map[Family]string{
	"libnss3.so":             {Debian: "libnss3", Fedora: "nss", Arch: "nss", SUSE: "mozilla-nss"},
	"libnssutil3.so":         {Debian: "libnss3", Fedora: "nss", Arch: "nss", SUSE: "mozilla-nss"},
	"libnspr4.so":            {Debian: "libnspr4", Fedora: "nspr", Arch: "nspr", SUSE: "mozilla-nspr"},
	"libatk-1.0.so.0":        {Debian: "libatk1.0-0", Fedora: "atk", Arch: "at-spi2-core", SUSE: "libatk-1_0-0"},
	"libatk-bridge-2.0.so.0": {Debian: "libatk-bridge2.0-0", Fedora: "at-spi2-atk", Arch: "at-spi2-core", SUSE: "libatk-bridge-2_0-0"},
	"libatspi.so.0":          {Debian: "libatspi2.0-0", Fedora: "at-spi2-core", Arch: "at-spi2-core", SUSE: "libatspi0"},
	"libcups.so.2":           {Debian: "libcups2", Fedora: "cups-libs", Arch: "libcups", SUSE: "libcups2"},
	"libdrm.so.2":            {Debian: "libdrm2", Fedora: "libdrm", Arch: "libdrm", SUSE: "libdrm2"},
	"libgbm.so.1":            {Debian: "libgbm1", Fedora: "mesa-libgbm", Arch: "mesa", SUSE: "libgbm1"},
	"libxkbcommon.so.0":      {Debian: "libxkbcommon0", Fedora: "libxkbcommon", Arch: "libxkbcommon", SUSE: "libxkbcommon0"},
	"libX11.so.6":            {Debian: "libx11-6", Fedora: "libX11", Arch: "libx11", SUSE: "libX11-6"},
	"libXcomposite.so.1":     {Debian: "libxcomposite1", Fedora: "libXcomposite", Arch: "libxcomposite", SUSE: "libXcomposite1"},
	"libXdamage.so.1":        {Debian: "libxdamage1", Fedora: "libXdamage", Arch: "libxdamage", SUSE: "libXdamage1"},
	"libXext.so.6":           {Debian: "libxext6", Fedora: "libXext", Arch: "libxext", SUSE: "libXext6"},
	"libXfixes.so.3":         {Debian: "libxfixes3", Fedora: "libXfixes", Arch: "libxfixes", SUSE: "libXfixes3"},
	"libXrandr.so.2":         {Debian: "libxrandr2", Fedora: "libXrandr", Arch: "libxrandr", SUSE: "libXrandr2"},
	"libxcb.so.1":            {Debian: "libxcb1", Fedora: "libxcb", Arch: "libxcb", SUSE: "libxcb1"},
	"libpango-1.0.so.0":      {Debian: "libpango-1.0-0", Fedora: "pango", Arch: "pango", SUSE: "libpango-1_0-0"},
	"libcairo.so.2":          {Debian: "libcairo2", Fedora: "cairo", Arch: "cairo", SUSE: "libcairo2"},
	"libasound.so.2":         {Debian: "libasound2", Fedora: "alsa-lib", Arch: "alsa-lib", SUSE: "libasound2"},
	"libdbus-1.so.3":         {Debian: "libdbus-1-3", Fedora: "dbus-libs", Arch: "dbus", SUSE: "libdbus-1-3"},
	"libexpat.so.1":          {Debian: "libexpat1", Fedora: "expat", Arch: "expat", SUSE: "libexpat1"},
	"libglib-2.0.so.0":       {Debian: "libglib2.0-0", Fedora: "glib2", Arch: "glib2", SUSE: "libglib-2_0-0"},
	"libgio-2.0.so.0":        {Debian: "libglib2.0-0", Fedora: "glib2", Arch: "glib2", SUSE: "libgio-2_0-0"},
	"libgobject-2.0.so.0":    {Debian: "libglib2.0-0", Fedora: "glib2", Arch: "glib2", SUSE: "libgobject-2_0-0"},
	"libfontconfig.so.1":     {Debian: "libfontconfig1", Fedora: "fontconfig", Arch: "fontconfig", SUSE: "libfontconfig1"},
	"libXi.so.6":             {Debian: "libxi6", Fedora: "libXi", Arch: "libxi", SUSE: "libXi6"},
	"libXtst.so.6":           {Debian: "libxtst6", Fedora: "libXtst", Arch: "libxtst", SUSE: "libXtst6"},
	"libXrender.so.1":        {Debian: "libxrender1", Fedora: "libXrender", Arch: "libxrender", SUSE: "libXrender1"},
	"libXcursor.so.1":        {Debian: "libxcursor1", Fedora: "libXcursor", Arch: "libxcursor", SUSE: "libXcursor1"},
	"libXss.so.1":            {Debian: "libxss1", Fedora: "libXScrnSaver", Arch: "libxss", SUSE: "libXss1"},
	"libgtk-3.so.0":          {Debian: "libgtk-3-0", Fedora: "gtk3", Arch: "gtk3", SUSE: "libgtk-3-0"},
	"libwayland-client.so.0": {Debian: "libwayland-client0", Fedora: "libwayland-client", Arch: "wayland", SUSE: "libwayland-client0"},
}

// InstallHint turns the missing libraries into the command that installs
// them on the given family. The second result is false when some library is
// not in the table; the command then covers the rest, and the names are
// still worth showing.
func InstallHint(missing []string, f Family) (string, bool) {
	seen := map[string]bool{}
	var pkgs []string
	complete := true
	for _, lib := range missing {
		pkg := libPackages[libKey(lib)][f]
		if pkg == "" {
			complete = false
			continue
		}
		if !seen[pkg] {
			seen[pkg] = true
			pkgs = append(pkgs, pkg)
		}
	}
	sort.Strings(pkgs)
	if len(pkgs) == 0 {
		return "", false
	}
	list := strings.Join(pkgs, " ")
	switch f {
	case Debian:
		return "sudo apt-get install -y " + list, complete
	case Fedora:
		return "sudo dnf install -y " + list, complete
	case Arch:
		return "sudo pacman -S --needed " + list, complete
	default:
		return "sudo zypper install " + list, complete
	}
}

// libKey strips a versioned soname such as libnss3.so.1 to its table key.
func libKey(lib string) string {
	if _, ok := libPackages[lib]; ok {
		return lib
	}
	if i := strings.Index(lib, ".so"); i >= 0 {
		base := lib[:i+3]
		if _, ok := libPackages[base]; ok {
			return base
		}
	}
	return lib
}

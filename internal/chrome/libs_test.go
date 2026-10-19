package chrome

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const lddOutput = `	linux-vdso.so.1 (0x00007ffd)
	libdl.so.2 => /lib/x86_64-linux-gnu/libdl.so.2 (0x00007f)
	libnss3.so => not found
	libnssutil3.so => not found
	libatk-1.0.so.0 => not found
	libgbm.so.1 => not found
	libmystery.so.9 => not found
	libc.so.6 => /lib/x86_64-linux-gnu/libc.so.6 (0x00007f)
`

func TestMissingLibsReadsLdd(t *testing.T) {
	var gotName string
	var gotArgs []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotName, gotArgs = name, args
		return []byte(lddOutput), nil
	}
	got, err := missingLibs(context.Background(), "/x/chrome", run)
	if err != nil {
		t.Fatal(err)
	}
	if gotName != "ldd" || len(gotArgs) != 1 || gotArgs[0] != "/x/chrome" {
		t.Errorf("ran %s %v", gotName, gotArgs)
	}
	want := "libatk-1.0.so.0 libgbm.so.1 libmystery.so.9 libnss3.so libnssutil3.so"
	if strings.Join(got, " ") != want {
		t.Errorf("missing = %v", got)
	}
	none, err := missingLibs(context.Background(), "/x/chrome", func(context.Context, string, ...string) ([]byte, error) {
		return []byte("\tlibc.so.6 => /lib/libc.so.6 (0x1)\n"), nil
	})
	if err != nil || len(none) != 0 {
		t.Errorf("nothing missing: %v, %v", none, err)
	}
	_, err = missingLibs(context.Background(), "/x/chrome", func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("executable file not found")
	})
	if err == nil || !strings.Contains(err.Error(), "ldd") {
		t.Errorf("no ldd: %v", err)
	}
}

func TestFamilyOf(t *testing.T) {
	cases := map[string]Family{
		"NAME=\"Ubuntu\"\nID=ubuntu\nID_LIKE=debian\n": Debian,
		"ID=debian\n": Debian,
		"ID=linuxmint\nID_LIKE=\"ubuntu debian\"\n": Debian,
		"ID=fedora\n": Fedora,
		"ID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\n": Fedora,
		"ID=arch\n":                      Arch,
		"ID=endeavouros\nID_LIKE=arch\n": Arch,
		"ID=opensuse-tumbleweed\nID_LIKE=\"opensuse suse\"\n": SUSE,
	}
	for in, want := range cases {
		if got, ok := FamilyOf(in); !ok || got != want {
			t.Errorf("FamilyOf(%q) = %q, %v, want %q", in, got, ok, want)
		}
	}
	if _, ok := FamilyOf("ID=alpine\n"); ok {
		t.Error("Alpine is not a supported family")
	}
	if _, ok := FamilyOf(""); ok {
		t.Error("an empty os-release was read")
	}
}

func TestInstallHint(t *testing.T) {
	missing := []string{"libatk-1.0.so.0", "libgbm.so.1", "libnss3.so", "libnssutil3.so"}
	cmd, complete := InstallHint(missing, Debian)
	if !complete || cmd != "sudo apt-get install -y libatk1.0-0 libgbm1 libnss3" {
		t.Errorf("debian: %q, %v", cmd, complete)
	}
	if cmd, _ := InstallHint(missing, Fedora); cmd != "sudo dnf install -y atk mesa-libgbm nss" {
		t.Errorf("fedora: %q", cmd)
	}
	if cmd, _ := InstallHint(missing, Arch); cmd != "sudo pacman -S --needed at-spi2-core mesa nss" {
		t.Errorf("arch: %q", cmd)
	}
	if cmd, _ := InstallHint(missing, SUSE); !strings.HasPrefix(cmd, "sudo zypper install ") {
		t.Errorf("suse: %q", cmd)
	}
	cmd, complete = InstallHint(append(missing, "libmystery.so.9"), Debian)
	if complete || !strings.Contains(cmd, "libnss3") {
		t.Errorf("an unknown library: %q, %v", cmd, complete)
	}
	if cmd, complete := InstallHint([]string{"libmystery.so.9"}, Debian); cmd != "" || complete {
		t.Errorf("only unknown libraries: %q, %v", cmd, complete)
	}
	// a versioned soname maps to the same package as its base
	if cmd, _ := InstallHint([]string{"libnss3.so.1"}, Debian); cmd != "sudo apt-get install -y libnss3" {
		t.Errorf("versioned soname: %q", cmd)
	}
	// every package name in the table is filled in for every family
	for lib, byFamily := range libPackages {
		for _, f := range []Family{Debian, Fedora, Arch, SUSE} {
			if byFamily[f] == "" {
				t.Errorf("%s has no %s package", lib, f)
			}
		}
	}
}

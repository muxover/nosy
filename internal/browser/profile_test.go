package browser

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func tempRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, k := range []string{"TMPDIR", "TEMP", "TMP"} {
		t.Setenv(k, dir)
	}
	return dir
}

func plantProfile(t *testing.T, root, name, owner string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(dir, "Default"), 0o700); err != nil {
		t.Fatal(err)
	}
	if owner != "" {
		if err := os.WriteFile(filepath.Join(dir, ownerFile), []byte(owner+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func endedProcess(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func TestNewProfileCarriesItsOwnersNote(t *testing.T) {
	tempRoot(t)
	dir, err := newProfile()
	if err != nil {
		t.Fatal(err)
	}
	defer removeProfile(dir)
	if isStale(dir) {
		t.Error("a profile of this process counts as left behind")
	}
	if err := removeProfile(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("the profile is still there after removeProfile")
	}
}

func TestClearStaleRemovesOnlyDeadOwnersProfiles(t *testing.T) {
	root := tempRoot(t)
	stale := plantProfile(t, root, profilePrefix+"stale", fmt.Sprint(endedProcess(t)))
	live := plantProfile(t, root, profilePrefix+"live", fmt.Sprint(os.Getpid()))
	noNote := plantProfile(t, root, profilePrefix+"nonote", "")
	garbled := plantProfile(t, root, profilePrefix+"garbled", "not a number")
	other := plantProfile(t, root, "other-"+profilePrefix, fmt.Sprint(endedProcess(t)))
	file := filepath.Join(root, profilePrefix+"file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	removed, problems := ClearStale()
	if removed != 1 || len(problems) != 0 {
		t.Fatalf("removed %d, problems %v, want 1 and none", removed, problems)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("the profile of a process that ended is still there")
	}
	for name, path := range map[string]string{"in use": live, "no note": noNote, "garbled note": garbled, "other name": other, "a file": file} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was removed: %v", name, err)
		}
	}
}

func TestClearStaleRemovesAProfileOlderThanAnyScan(t *testing.T) {
	root := tempRoot(t)
	old := plantProfile(t, root, profilePrefix+"old", fmt.Sprint(os.Getpid()))
	when := time.Now().Add(-2 * profileMaxAge)
	if err := os.Chtimes(filepath.Join(old, ownerFile), when, when); err != nil {
		t.Fatal(err)
	}
	if removed, _ := ClearStale(); removed != 1 {
		t.Errorf("removed %d, want the old profile gone even though its process number is in use", removed)
	}
}

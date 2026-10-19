package browser

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	profilePrefix = "nosy-profile-"
	ownerFile     = ".nosy-owner"
	// a scan lasts two minutes at most, so a profile this old is left over even
	// if its owner's process number has been given to another program
	profileMaxAge = time.Hour
)

// newProfile makes the throwaway profile of one scan, with a note of who owns
// it so a later run can tell it from one still in use.
func newProfile() (string, error) {
	dir, err := os.MkdirTemp("", profilePrefix)
	if err != nil {
		return "", err
	}
	owner := fmt.Sprintf("%d\n", os.Getpid())
	if err := os.WriteFile(filepath.Join(dir, ownerFile), []byte(owner), 0o600); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

// removeProfile deletes a profile. The browser's helper processes may still be
// closing files in it just after the browser is killed, so a failed removal is
// retried for a couple of seconds.
func removeProfile(dir string) error {
	var err error
	for i := 0; i < 20; i++ {
		if err = os.RemoveAll(dir); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return err
}

// ClearStale removes the profiles earlier scans left behind when they were
// killed or crashed. Only folders in the temp directory that carry Nosy's
// name and its own note, and whose owner is gone, are touched; a profile of
// a scan running right now, or any folder that is not Nosy's, stays.
func ClearStale() (removed int, problems []error) {
	root := os.TempDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, []error{err}
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), profilePrefix) {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if !isStale(dir) {
			continue
		}
		if err := removeProfile(dir); err != nil {
			problems = append(problems, fmt.Errorf("remove the old browser profile %s: %w", dir, err))
			continue
		}
		removed++
	}
	return removed, problems
}

// isStale is true for a folder with Nosy's note in it whose owner is gone.
func isStale(dir string) bool {
	note := filepath.Join(dir, ownerFile)
	info, err := os.Lstat(note)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if time.Since(info.ModTime()) > profileMaxAge {
		return true
	}
	b, err := os.ReadFile(note)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return false
	}
	return !processAlive(pid)
}

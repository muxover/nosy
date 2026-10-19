package chrome

import (
	"fmt"
	"os/exec"
)

// allowSandboxedProcesses gives the browser's sandboxed helper processes read
// access to the install, the way Chrome's own installer does. A build unpacked
// into a user folder lacks it, and the helpers then cannot start.
func allowSandboxedProcesses(dir string) error {
	out, err := exec.Command("icacls", dir,
		"/grant", "*S-1-15-2-1:(OI)(CI)(RX)",
		"/grant", "*S-1-15-2-2:(OI)(CI)(RX)",
		"/T", "/C", "/Q").CombinedOutput()
	if err != nil {
		return fmt.Errorf("let the browser's helper processes read the install: %w: %s", err, out)
	}
	return nil
}

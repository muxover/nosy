//go:build nosytest

package browser

import (
	"net/url"
	"os"
	"os/exec"
	"time"

	"github.com/chromedp/chromedp"
)

var trustedSPKI string

// SetTestTrust makes the browser accept certificates issued by the test CA
// with this SPKI hash. Test builds only.
func SetTestTrust(spki string) { trustedSPKI = spki }

// testFlags are the test builds' way to point at a browser (NOSY_CHROME). The
// sandbox cannot be turned off, in tests either.
func testFlags() []chromedp.ExecAllocatorOption {
	var opts []chromedp.ExecAllocatorOption
	if trustedSPKI != "" {
		opts = append(opts, chromedp.Flag("ignore-certificate-errors-spki-list", trustedSPKI))
	}
	if p := os.Getenv("NOSY_CHROME"); p != "" {
		opts = append(opts, chromedp.ExecPath(p))
	}
	return opts
}

// Started, when set, is called with the browser's process number once it runs.
var Started func(pid int)

func started(cmd *exec.Cmd) {
	if Started != nil && cmd != nil && cmd.Process != nil {
		Started(cmd.Process.Pid)
	}
}

// cutShortHost is a planted page whose storage read is given almost no time,
// so that a read cut short can be tested on any machine.
const cutShortHost = "cutshort.test"

func budgetFor(target *url.URL, budget time.Duration) time.Duration {
	if target != nil && target.Hostname() == cutShortHost {
		return time.Millisecond
	}
	return budget
}

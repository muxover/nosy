//go:build nosytest

package browser

import (
	"os"

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

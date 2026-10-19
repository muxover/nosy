package chrome

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// MinMajor is the oldest Chrome major version Nosy supports. The driver relies
// on the target auto-attach filters, user agent metadata and the third-party
// cookie controls that exist from this version on.
const MinMajor = 120

// ErrTooOld means the browser is older than MinMajor.
var ErrTooOld = errors.New("the browser is too old")

// Major reads the major version from a product string such as
// "Chrome/155.0.8059.39" or "HeadlessChrome/155.0.8059.39".
func Major(product string) (int, bool) {
	name, v, ok := strings.Cut(product, "/")
	if !ok || name == "" {
		return 0, false
	}
	major, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(major)
	return n, err == nil
}

// CheckVersion refuses a browser older than MinMajor.
func CheckVersion(product string) error {
	n, ok := Major(product)
	if !ok {
		return fmt.Errorf("cannot read the browser version from %q", product)
	}
	if n < MinMajor {
		return fmt.Errorf("%w: %s is older than Chrome %d, the oldest Nosy supports. Update it, or run \"nosy browser install\"", ErrTooOld, product, MinMajor)
	}
	return nil
}

//go:build !nosytest

package integration

import "testing"

func TestBrowserSuite(t *testing.T) {
	t.Skip("the planted-page tests need -tags nosytest and NOSY_BROWSER_TESTS=1 (see internal/integration/doc.go)")
}

//go:build nosytest

package main

import (
	"os"

	"github.com/muxover/nosy/internal/testsite"
)

// A test build can be pointed at a planted-page server running in another
// process (the test that started it): NOSY_TEST_HTTP and NOSY_TEST_TLS are its
// addresses and NOSY_TEST_SPKI the pin of its CA. NOSY_TEST_CONFIG is the
// config file to use.
func init() {
	if http := os.Getenv("NOSY_TEST_HTTP"); http != "" {
		testsite.Register(http, os.Getenv("NOSY_TEST_TLS"), os.Getenv("NOSY_TEST_SPKI"))
	}
	if path := os.Getenv("NOSY_TEST_CONFIG"); path != "" {
		configFile = func() (string, error) { return path, nil }
	}
}

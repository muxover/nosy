// Package integration holds the planted-page tests. A local server serves
// pages that set known trackers, cookies, fingerprinting calls and typed-text
// leaks, a real Chromium visits them through the guard proxy, and each test
// asserts that the engine caught what was planted and nothing else.
//
// The tests need a browser. They are compiled only with -tags nosytest, which
// also switches on the guard's test hooks, and they run only when
// NOSY_BROWSER_TESTS=1 is set. Without that they skip with a message. With it
// set, a missing browser fails the run instead of skipping it.
package integration

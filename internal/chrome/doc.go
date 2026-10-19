// Package chrome decides which browser a scan uses and installs Nosy's own.
//
// The choice, in order: the browser set in the config, the browser named by
// NOSY_CHROME, Nosy's pinned Chrome for Testing build if it is installed, and
// last a Chrome or Chromium found in the usual places. Other Chromium
// browsers (Edge, Brave, Opera) are never found by themselves, only when a
// path is given: each ships its own tracking protection and update traffic, so
// a scan in one would measure the browser as much as the site.
//
// The pinned build is downloaded from Google's Chrome for Testing URLs and
// checked against a SHA-256 kept in the code before a byte of it is unpacked.
package chrome

# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Project scaffold: Go module, `nosy version` and `nosy help`, CI, and release workflow.
- Scan engine, not yet reachable from the CLI: Chromium driver with a fresh profile or context per visit, guard proxy that blocks private addresses, capture, detectors and privacy score formula v1.
- Planted-page tests that run a real Chromium on Linux, Windows and macOS in CI.
- `nosy scan <url>` with `--duration`, `--device`, `--gpc`, `--allow-private`, `--json` and `--quiet`, a plain terminal summary with observed, classified and inferred findings, and a progress line on terminals.
- `nosy check <url>`, `nosy config list|get|set|path`, `nosy doctor`, and the exit codes 0, 1, 2 and 130.
- Mobile emulation, the Global Privacy Control signal and a configurable visit duration in the browser driver.
- A manual `Timing` workflow that times one scan of a public page.

- `nosy browser install` downloads a pinned Chrome for Testing (155.0.8059.84) from Google's official URLs into the user cache and checks its SHA-256 before unpacking; `nosy browser path` shows which browser a scan would use and why; `nosy browser remove` deletes only what Nosy downloaded.
- Browser choice in a fixed order (the `chrome` setting, `NOSY_CHROME`, the pinned build, a system Chrome or Chromium), a minimum of Chrome 120, and a record in every result of which browser was used and whether it was the pinned build.
- `nosy doctor` checks the browser's version, its Linux system libraries (with the install command for Debian and Ubuntu, Fedora, Arch and openSUSE) and its sandbox.
- Storage capture: localStorage, sessionStorage, IndexedDB, Cache Storage names and service workers, per frame origin with first and third party apart, with values that look like identifiers marked by the cookie rule.
- A manual `Chrome hashes` workflow that prints the size and SHA-256 of each Chrome for Testing build to pin.

- Site isolation is on. Frames of other sites, frames inside them and sandboxed frames are watched with the same instrumentation as the page: requests, cookies, storage, fingerprinting, session recording and typed text found in them are attributed to the frame's origin.
- Findings for new windows, downloads and addresses on the machine that a page tried to reach, and a `refused` section in the JSON result.
- `webgl` (hardware, software or none) and `storage_cut_short` in the result's conditions, and a note and a line in the terminal when the storage read ran out of time.
- Throwaway profiles carry a note of their owner; profiles left by a crashed or killed scan are cleared by the next scan, and only those.
- Downloads are refused, every permission request denied, and new windows closed before they load anything.

### Changed

- On Windows, `nosy browser install` gives the downloaded browser's helper processes read access to their folder, as Chrome's installer does. Without it the pinned build could not start its network process from a user folder.
- Software WebGL stays on as a fallback for machines with no GPU, and the result says which kind a scan got.
- The shared browser and browser contexts are gone: every scan is its own browser process with its own profile.

- The `chrome` setting now comes before `NOSY_CHROME`.
- The browser's sandbox can never be turned off. chromedp turned it off by itself for a process running as root; every launch now keeps it on, and the test builds no longer have a switch for it.
- The browser and its helpers are taken down together when a scan ends or is interrupted, on Windows too, and Ctrl+C is tested on Windows.

- CI no longer scans public sites, and pins staticcheck to a fixed version.

[Unreleased]: https://github.com/muxover/nosy/commits/main

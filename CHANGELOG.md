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

### Changed

- CI no longer scans public sites, and pins staticcheck to a fixed version.

[Unreleased]: https://github.com/muxover/nosy/commits/main

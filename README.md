# Nosy

<div align="center">

[![CI](https://github.com/muxover/nosy/actions/workflows/ci.yml/badge.svg)](https://github.com/muxover/nosy/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-GPL--3.0-blue.svg)](LICENSE)

**Find out what a website does to its visitors.**

</div>

---

Nosy is an open-source CLI that investigates a website on your own machine, in a clean throwaway browser. `nosy scan <url>` records requests, companies, cookies, storage, fingerprinting, session recording and typed-text leaks, and runs setup checks that need no browser: HTTPS, headers, first cookies, ads.txt, page source, SPF and DMARC.

**Status:** in development. The first release is v0.1.0. Only `nosy version` and `nosy help` work today.

---

## Features

- Every scan runs in an isolated Chromium with a new temporary profile.
- All browser traffic goes through a local guard proxy that blocks private addresses.
- Two grades, never mixed: a 0-100 privacy score (public formula v1) and an A-F setup grade.
- Every finding is labelled observed, classified or inferred, with its evidence.
- Every report records its conditions: date, country, browser and version, device, consent mode, duration, Nosy and rules versions.
- Output is a terminal summary, a single-file offline HTML report and a versioned JSON report.
- Detection rules come from [nosy-rules](https://github.com/muxover/nosy-rules): EasyPrivacy, AdGuard companiesdb and Nosy overrides.
- No telemetry, no server-side scanning, free.

---

## Installation

Coming soon with v0.1.0.

---

## Commands

| Command | Description |
|---------|-------------|
| `nosy version` | Print the version |
| `nosy help` | Show usage |

---

## Project Layout

```text
nosy/
├── cmd/nosy/            # CLI entry point
├── internal/
│   ├── browser/         # Chromium driver: isolated visits, frame and worker capture, page instrumentation
│   ├── capture/         # What one visit recorded: requests, cookies, API calls, page facts
│   ├── detect/          # Companies, trackers, identifier cookies, session recording, typed-text leaks, bot checks
│   ├── guard/           # Local proxy that blocks private addresses and pins DNS
│   ├── integration/     # Planted-page tests against a real Chromium
│   ├── score/           # Privacy score, formula v1
│   └── testsite/        # Planted test pages, test builds only
├── release-notes/       # One file per release
└── docs/PROJECT.md      # Maintainer notes
```

---

## License

Licensed under the [GPL-3.0](LICENSE) license.

---

## Links

- Repository: https://github.com/muxover/nosy
- Issues: https://github.com/muxover/nosy/issues
- Changelog: [CHANGELOG.md](CHANGELOG.md)

---

<p align="center">Made with ❤️ by Jax (@muxover)</p>

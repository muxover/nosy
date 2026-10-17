# Nosy

<div align="center">

[![CI](https://github.com/muxover/nosy/actions/workflows/ci.yml/badge.svg)](https://github.com/muxover/nosy/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-GPL--3.0-blue.svg)](LICENSE)

**Find out what a website does to its visitors.**

</div>

---

Nosy is an open-source CLI that investigates a website on your own machine, in a clean throwaway browser. `nosy scan <url>` records requests, companies, cookies, storage, fingerprinting, session recording and typed-text leaks, and runs setup checks that need no browser: HTTPS, headers, first cookies, ads.txt, page source, SPF and DMARC.

**Status:** in development. The first release is v0.1.0. `nosy scan`, `check`, `config` and `doctor` work. Tracker rules, the HTML and JSON reports and `nosy browser install` come before v0.1.0, so today a scan lists the companies a page contacts without classifying them.

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

## Quick Start

Nosy needs Google Chrome or Chromium. Check that it finds one:

```text
nosy doctor
```

Scan a site, or look at it without a browser:

```text
nosy scan example.com
nosy scan --device mobile --gpc --json example.com
nosy check example.com
```

A scan opens the site in a new throwaway Chrome profile, scrolls and types into its fields like a reader, and prints a summary: the privacy score, request and company counts, the main findings each labelled observed, classified or inferred, and the conditions of the scan. Addresses on your own network are refused unless you pass `--allow-private`.

---

## Commands

| Command | Description |
|---------|-------------|
| `nosy scan <url>` | Open a site in a clean Chrome and report what it does |
| `nosy check <url>` | Look at a site without a browser: where it leads, what it answers, its first cookies |
| `nosy config list\|get\|set\|path` | Show or change settings |
| `nosy doctor` | Check that Chrome is found and can start with its sandbox |
| `nosy version` | Print the version |
| `nosy help [command]` | Show help |

---

## Flags

| Command | Flag | Description |
|---------|------|-------------|
| `scan` | `--duration D` | Longest the visit may last, 10s to 120s (default 20s) |
| `scan` | `--device D` | `desktop` or `mobile` (default `desktop`) |
| `scan` | `--gpc` | Send the Global Privacy Control signal |
| `scan`, `check` | `--allow-private` | Allow private, loopback and link-local targets; prints a warning |
| `scan`, `check` | `--json` | Print the result as JSON on stdout |
| `scan` | `--quiet` | No progress, and a one-line summary |

Progress is drawn on stderr, and only when stderr is a terminal. Exit codes: 0 done, 1 the scan or check failed (including a blocked page), 2 wrong usage, 130 interrupted with Ctrl+C.

---

## Configuration

| Setting | Default | Description |
|---------|---------|-------------|
| `chrome` | empty | Path of Chrome or Chromium; empty searches the usual places |
| `device` | `desktop` | Default for `--device` |
| `duration` | `20` | Default for `--duration`, in seconds |
| `gpc` | `false` | Default for `--gpc` |

Settings live in `config.json` in your user config directory (`nosy config path`). The `NOSY_CHROME` environment variable overrides `chrome`.

---

## Project Layout

```text
nosy/
├── cmd/nosy/            # The commands: scan, check, config, doctor
├── internal/
│   ├── browser/         # Chromium driver: isolated visits, frame and worker capture, page instrumentation
│   ├── capture/         # What one visit recorded: requests, cookies, API calls, page facts
│   ├── check/           # The look at a site that needs no browser
│   ├── detect/          # Companies, trackers, identifier cookies, session recording, typed-text leaks, bot checks
│   ├── guard/           # Local proxy that blocks private addresses and pins DNS
│   ├── integration/     # Planted-page tests against a real Chromium
│   ├── scan/            # One scan, from address to result
│   ├── score/           # Privacy score, formula v1
│   ├── site/            # Parsing and validating the address a user types
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

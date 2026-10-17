# Nosy — maintainer notes

## What this is

Nosy is an open-source CLI that investigates what a website does to its visitors, on the user's own machine, in a clean throwaway browser.

`nosy scan <url>` opens the site in an isolated Chromium. Every scan gets a new temporary profile, and all traffic goes through a local guard proxy that blocks private addresses. Nosy records requests, companies, cookies, storage, fingerprinting, session recording and typed-text leaks. It also runs setup checks that need no browser: HTTPS, headers, first cookies, ads.txt, page source, SPF, DMARC and similar.

Output is a terminal summary, a single-file offline HTML report and a versioned JSON report.

### Principles

- Two grades, never mixed: a 0-100 privacy score (public formula v1) and an A-F setup grade.
- Every finding is labelled observed, classified or inferred, with its evidence.
- Every report records its conditions: date, country, browser and version, device, consent mode, duration, Nosy version and rules version.
- Detection rules come from [muxover/nosy-rules](https://github.com/muxover/nosy-rules): EasyPrivacy, AdGuard companiesdb and Nosy overrides (CC BY-SA, signed). DuckDuckGo Tracker Radar is not used.
- No telemetry, no server-side scanning, free.
- Sharing to the public index at nosy.muxover.com is opt-in: asked on first run, default No, switched with `nosy config share on|off`. It ships in v0.2.0, not before.

## Stack and layout

- Go (current stable), one module: `github.com/muxover/nosy`. Dependencies: `chromedp` and `cdproto` for the browser, `gobwas/ws` for the second DevTools connection, `x/net` for the public suffix list.
- `cmd/nosy/` holds the entry point. It has only `version` and `help`; `nosy scan` is step 4.
- The engine lives under `internal/` and has no public API yet:
  - `internal/browser`: the Chromium driver. A visit gets a new temporary profile, or a browser context of its own in a browser that stays up. A second DevTools connection (`Target.setAutoAttach`, paused on start) attaches frames and workers before their first line runs. `instrument.js` is added with `Page.addScriptToEvaluateOnNewDocument` and reports through a `Runtime.addBinding` the page cannot see afterwards.
  - `internal/guard`: the proxy every browser byte goes through. It resolves names itself, refuses private, loopback, link-local, metadata and reserved addresses and any port but 80 and 443, checks the address again on the socket before connect, and counts bytes against the size cap.
  - `internal/capture`: plain data for one visit.
  - `internal/detect`: company and tracker matching, identifier cookies, fingerprinting, session recording, typed-text leaks, bot checks and thin pages.
  - `internal/score`: formula v1, unchanged from the old engine.
  - `internal/testsite`: the planted pages. `internal/integration`: the tests that visit them.
- Test builds use `-tags nosytest`. That tag compiles in the guard's routing and DNS hooks and the planted-page server, and nothing else compiles them in. The browser tests also need `NOSY_BROWSER_TESTS=1`; without it they skip with a message. `NOSY_CHROME` names the browser, `NOSY_NO_SANDBOX=1` runs it without its sandbox where the machine forbids one, `NOSY_PARALLEL` caps the browsers open at once.
- Old code lives in the private `muxover/nosy-legacy` repo and is read only.
- Rules live in `muxover/nosy-rules`.

## Decisions

- 2026-10-17: licence is GPL-3.0.
- 2026-10-17: the new engine matches the old one on golden sites when the median company count is within 10% and the score is within 5 points.
- 2026-10-17: the coming-soon page stays up until v0.1.0.
- 2026-10-17: the public index is v0.2.0 scope. v0.1.0 has no sharing code.
- 2026-10-17: the engine is rewritten into this module package by package, not copied. Behaviour is kept, including the score formula, so step 11 can compare the two engines.
- 2026-10-17: no list data is committed. The domain index, the CDN blocklist and the replay vendor list keep the shape the old engine read, and the tests build tiny invented ones (all `.test` domains). The old engine's data came from DuckDuckGo Tracker Radar and the DuckDuckGo tracker blocklist (CC BY-NC-SA); rules v1 replaces both in step 8, and neither data set goes into this repo.
- 2026-10-17: the session-replay vendor list (`internal/detect/replay_vendors.json`) is Nosy's own, each entry sourced from the vendor's documentation, and is ported as it was.
- 2026-10-17: Chromium 141 blocks third-party cookies with the reason `ThirdPartyPhaseout` whatever the profile says, which hid every tracker cookie from the scan. The browser starts with `TrackingProtection3pcd` disabled, so a scan behaves like a default desktop Chrome again.
- 2026-10-17: the old engine killed the whole browser process tree on Linux only. On macOS the helpers outlived the browser and the profile directory survived the hard stop. The browser now runs in its own process group on every Unix system, and removing the profile is retried for two seconds.
- 2026-10-17: the planted-page tests build a small pipeline of their own (proxy, visit, detectors, score) in `internal/integration/engine_test.go`. Step 4 replaces it with the real scan code and moves the tests onto it.
- 2026-10-17: work happens one build step at a time on the `build` branch and is reviewed before the next step. Nothing is tagged or released from it.

## Status

Steps 1 to 3 are done. The engine packages are in and tested against a real Chromium on Linux, Windows and macOS. `nosy` itself still has only `version` and `help`.

What was left behind on purpose, because it is server-side or belongs to a later step: the web app, store, queue, worker, share cards, Turnstile and rate limits; the shared-browser pool and its replacement logic; the live request feed; the report model and renderers (step 10); technology detection from webappanalyzer rules, so consent platforms found that way are not reported, only the consent APIs the page exposes; country lookup from IP ranges; input parsing and validation of the link a user types (`site.Parse`, step 4); the scan retry loop and report assembly (step 4).

Not in the old engine either, so not ported: reading localStorage, sessionStorage and IndexedDB. The brief lists storage, so it needs building (step 5 or 10).

Behaviour that differs from the old engine:

- The page lookups for technology rules (`window` paths and DOM selectors) are no longer collected, and `capture.Page` lost its `JS` and `DOM` fields. Page source, scripts, meta tags and response headers are still collected, for the setup checks.
- `browser.Options` lost `Probes` and `Seen`. `guard.StartProxy` no longer takes a listen or advertise address, since a browser in another container is not a case for a desktop tool; a browser that stays up on the same machine still works through `Options.Remote`.
- `detect.DetectConsent` takes platform names instead of technology detections.
- The tracker classes are `detect.Class` constants with a `Class` prefix, because `detect.Replay` is a function.
- The browser starts with third-party cookie tracking protection off (see Decisions).

## Next

Step 4: the CLI commands, starting with `nosy scan`, on top of the engine.

## v0.1.0 build order

### 1. Save old code

Done. The previous implementation is kept in `muxover/nosy-legacy`.

### 2. Scaffold

Module, entry point with `version` and `help`, repo files, CI and release workflow, repo settings.

### 3. Port engine

Done. Browser driver, guard, capture, detect and score, ported from the old code. See Status for what was left behind and what changed.

### 4. CLI commands

The command set around `scan`.

### 5. Browser handling

### 6. Isolation

### 7. Guard on desktop

### 8. Rules v1

### 9. Setup checks

### 10. Reports

### 11. Golden comparison

Compare against the old engine within the tolerance above.

### 12. Packaging

### 13. Docs site

### 14. Release

## Open questions

- Site isolation is disabled (`IsolateOrigins`, `site-per-process`, `disable-site-isolation-trials`) and `enable-unsafe-swiftshader` is on. Both were inherited from the server container: isolation off puts cross-site iframe requests on the page's own DevTools session, and software WebGL stands in for a GPU the server lacks. On a desktop, site isolation must be on unless step 6 proves capture cannot work with it. Step 6 decides, with tests for out-of-process iframes.
- The category names the index uses (`Advertising`, `Ad Motivated Tracking`, `CDN` and so on) are the old data's. Rules v1 has its own vocabulary, so `detect.ClassOf` and the CDN rule change in step 8.
- Where the browser-only scan checks run once the old container assumptions are gone (step 5), and whether `Options.Remote` survives step 6.

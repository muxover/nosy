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
- `cmd/nosy/` is the command line, on the standard library alone (`flag`, `encoding/json`): `main.go` routes, `scan.go`, `check.go`, `browser.go`, `config.go` and `doctor.go` are the commands, `print.go` the plain terminal output, `term.go` the progress line, `flags.go` the flag helpers. An `app` value holds what a command takes from the machine (streams, environment, config path), so tests run the commands in process.
- The engine lives under `internal/` and has no public API yet:
  - `internal/browser`: the Chromium driver. At the end of a visit it also reads what each frame origin stored: `storage.js` runs in a world of its own in every frame, so a third-party frame is read from its own partition. A visit gets a new temporary profile, or a browser context of its own in a browser that stays up. A second DevTools connection (`Target.setAutoAttach`, paused on start) attaches frames and workers before their first line runs. `instrument.js` is added with `Page.addScriptToEvaluateOnNewDocument` and reports through a `Runtime.addBinding` the page cannot see afterwards.
  - `internal/guard`: the proxy every browser byte goes through. It resolves names itself, refuses private, loopback, link-local, metadata and reserved addresses and any port but 80 and 443, checks the address again on the socket before connect, and counts bytes against the size cap.
  - `internal/scan`: one scan, from the address to the `scan.Result`. It parses the address, checks it with the guard, runs the visit (repeated while the browser's network restarts, and once more if the page was blocked), runs the detectors and scores. `Build` turns a capture into a result without a browser.
  - `internal/chrome`: which browser a scan uses and Nosy's own. `Select` applies the order below, `Installer` downloads the pinned Chrome for Testing, checks its SHA-256 and unpacks it, `MissingLibs` and `InstallHint` find the system libraries a Linux Chrome lacks, `CheckVersion` enforces the minimum.
  - `internal/check`: the browser-less look at a site for `nosy check`, through the guard's dialer.
  - `internal/site`: parsing and validating the address a user types.
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
- 2026-10-17: the planted-page tests run on `scan.Run`, the code `nosy scan` runs, with the invented rules passed in through `scan.Options`.
- 2026-10-17: no argument-parsing dependency. The standard `flag` package covers the commands; the one gap, flags after the address (`nosy scan example.com --json`), is closed by parsing in a loop in `flags.go`. The config is JSON for the same reason: a TOML or YAML library would be a dependency for four settings.
- 2026-10-17: `nosy config` has `chrome`, `device`, `duration` and `gpc`, the defaults of the matching scan flags; a flag beats the config, and `NOSY_CHROME` beats the `chrome` setting. There is no sharing setting before v0.2.0. The file is `config.json` in the OS user config directory, written with mode 0600.
- 2026-10-17: a finding says how it is known. Observed: the browser saw it happen (a company was contacted, a cookie was set, typed text left the page). Classified: observed, then matched to a rule or list (a tracking company, an identifier cookie, a fingerprinting technique, a session-recording vendor). Inferred: concluded from a pattern (a thin page is probably a bot check).
- 2026-10-17: the conditions of every result record the third-party cookie setting (`TrackingProtection3pcd` disabled), site isolation off and software WebGL on, next to the date, browser, device, viewport, consent mode, GPC, duration and the Nosy and rules versions. The country is `unknown` until a location source is chosen.
- 2026-10-17: `--allow-private` lets the guard reach private, loopback and link-local addresses and any port, and a bare local address gets `http://`. Names are still resolved by the guard. The warning goes to stderr on every use.
- 2026-10-17: a wrong address is a usage error (exit 2), checked before the browser is looked for, so it reads the same on a machine with no Chrome. A page that was blocked or could not be loaded exits 1.
- 2026-10-17: `--duration` is the hard stop of the visit, 10 to 120 seconds. The wait for a quiet network is the same share of it as before, 8.5 of 20 seconds, and the other phases keep their lengths.
- 2026-10-17: `--device mobile` is a 390 by 844 screen at three times the pixel density with touch and a mobile user agent (reduced Chrome for Android). `--gpc` sends `Sec-GPC: 1` on the page's requests and sets `navigator.globalPrivacyControl`; requests made by workers do not carry the header.
- 2026-10-17: `nosy check` is deliberately small. The old detect package had no browser-less checks, so it uses the guard's dialer for one GET of the home page (headers only) and reads the redirects and the cookies the first answer sets with the cookie rules in `detect`. The setup checks proper are step 9.
- 2026-10-19: `nosy check` stays small until step 9 replaces it. Refusing 127.0.0.1 with exit 2 is right. The result's country stays `unknown` until step 8, when the DB-IP Lite data arrives.
- 2026-10-19: no sandbox opt-out, ever. Nosy opens untrusted sites on the user's own machine. `nosy doctor` says what is wrong and how to fix it; a Docker mode may come later as the safe alternative. The test build has no switch either: CI turns on the user namespaces the sandbox needs (`kernel.apparmor_restrict_unprivileged_userns=0`) instead, and local runs use a non-root user. Found on the way: chromedp adds `--no-sandbox` by itself when the process runs as root. Every launch now sets the flag to false first, and a test fails if a launch or a probe ever passes it. `Probe` starts the browser once, with the sandbox, and reads the browser's own output to tell a sandbox failure from other failures; it never retries without the sandbox.
- 2026-10-19: the browser is chosen in this order: the `chrome` setting, `NOSY_CHROME`, Nosy's pinned build if installed, then a Chrome or Chromium found in the usual places. (In step 4 `NOSY_CHROME` came before the setting.) A path the user set must exist; a wrong one is an error, never a quiet fall back. The result records `browser_source` (config, env, pinned or system) and `browser_pinned`, never the path.
- 2026-10-19: one rule for other Chromium browsers: Nosy finds Google Chrome and Chromium by itself and any other (Edge, Brave, Opera, Vivaldi) only when given a path. They ship their own tracking protection, ad blocking and update traffic, which change what a page does and what a scan measures, so using one has to be the user's decision. `doctor` warns when the browser does not call itself Chrome or Chromium.
- 2026-10-19: the oldest supported browser is Chrome 120 (`chrome.MinMajor`). The driver relies on the target auto-attach filters, user agent metadata and the third-party cookie controls from that version. An older browser is refused with a message that names the minimum and `nosy browser install`, by `doctor` and by a scan.
- 2026-10-19: the pinned build is Chrome for Testing 155.0.8059.84 for linux64, mac-arm64, mac-x64 and win64. There is no Chrome for Testing for Linux arm64 or Windows arm64, so there `nosy browser install` says so and points at a system Chromium. The size and SHA-256 of each zip come from the `Chrome hashes` workflow, which downloads Google's own URLs (`https://storage.googleapis.com/chrome-for-testing-public/<version>/<platform>/chrome-<platform>.zip`) on a GitHub runner and prints them; they are pinned in `internal/chrome/pins.go`. To move the pin, run that workflow with the new version and paste its output.
- 2026-10-19: `nosy browser install` downloads only from `storage.googleapis.com` over HTTPS and refuses a redirect to any other host. It reads no more than the pinned size, checks the SHA-256 before a byte is unpacked, unpacks into a staging folder (refusing paths and links that leave it), and moves it into `<user cache>/nosy/chrome/<version>/` in one step. `--force` downloads again; `nosy browser remove` deletes that `nosy/chrome` folder and nothing else, and refuses any other folder.
- 2026-10-19: on Linux `doctor` runs `ldd` on the browser and names the missing libraries with the install command for Debian and Ubuntu, Fedora and RHEL, Arch and openSUSE. A library that is not in the table is still listed.
- 2026-10-19: storage capture reads localStorage, sessionStorage, IndexedDB (databases, versions, object stores, entry counts, the first keys and string values), Cache Storage names and registered service workers, per frame origin, with first and third party apart. Values are checked with the cookie identifier rule (`detect.LooksLikeIdentifier`) and are never written to a result. A sessionStorage value is gone with the tab, so by the cookie rule for session cookies it is listed but never an identifier. Storage is observation: it does not change the score. Limits: 20 frames, 50 keys, 25 IndexedDB keys and 20 entries per store, 300 characters per value.
- 2026-10-19: where the browser-only checks run: in the one visit, in the one browser Nosy starts on the user's machine, collected at the end of the visit and analysed afterwards by `scan.Build` from the capture. The checks that need no browser (`nosy check` now, the setup checks of step 9) run in the Go process through the guard's dialer. There is no shared browser, pool or replacement logic. `Options.Remote` stays for the isolation tests until step 6 decides.
- 2026-10-19: the browser and everything it started are taken down when the scan's context ends: on Unix the whole process group, on Windows `taskkill /T` from the command's `Cancel` hook while the main process is still alive to name the tree. Ctrl+C on Windows is tested by starting the command with `CREATE_NEW_PROCESS_GROUP` and sending `CTRL_BREAK_EVENT`, which Go delivers as `os.Interrupt`.
- 2026-10-17: work happens one build step at a time on the `build` branch and is reviewed before the next step. Nothing is tagged or released from it.

## Status

Steps 1 to 5 are done. `nosy scan`, `check`, `browser install|path|remove`, `config`, `doctor`, `version` and `help` work, and the engine behind them is tested against a real Chromium on Linux, Windows and macOS, both a Chrome already on the machine and Nosy's pinned build.

Until rules v1 (step 8) there is no tracker data: a scan lists the companies it contacted and notes that none is classified, so scores come from fingerprinting, cookies, session recording and typed-text leaks only. Storage is reported but not scored. `scan.Result` is the type the reports will render from; its JSON layout (`nosy-scan/0`) is provisional until step 10, and the conditions already record the browser, whether it was the pinned build, the third-party cookie setting, site isolation and software WebGL.

What was left behind on purpose, because it is server-side or belongs to a later step: the web app, store, queue, worker, share cards, Turnstile and rate limits; the shared-browser pool and its replacement logic; the live request feed; the report renderers (step 10); technology detection from webappanalyzer rules, so consent platforms found that way are not reported, only the consent APIs the page exposes; country lookup from IP ranges (step 8).

Behaviour that differs from the old engine:

- The page lookups for technology rules (`window` paths and DOM selectors) are no longer collected, and `capture.Page` lost its `JS` and `DOM` fields. Page source, scripts, meta tags and response headers are still collected, for the setup checks.
- `browser.Options` lost `Probes` and `Seen`. `guard.StartProxy` no longer takes a listen or advertise address.
- `detect.DetectConsent` takes platform names instead of technology detections.
- The tracker classes are `detect.Class` constants with a `Class` prefix, because `detect.Replay` is a function.
- The browser starts with third-party cookie tracking protection off (see Decisions), and the sandbox can never be turned off.

## Next

Step 6: isolation. Turn site isolation on, keep capture working for out-of-process iframes and workers, test it, decide the fate of `Options.Remote`.

## v0.1.0 build order

### 1. Save old code

Done. The previous implementation is kept in `muxover/nosy-legacy`.

### 2. Scaffold

Module, entry point with `version` and `help`, repo files, CI and release workflow, repo settings.

### 3. Port engine

Done. Browser driver, guard, capture, detect and score, ported from the old code. See Status for what was left behind and what changed.

### 4. CLI commands

Done. `scan`, `check`, `config`, `doctor`, with exit codes 0, 1, 2 and 130. `report`, `rules update` and `browser install` are not registered; they come in steps 10, 8 and 5.

### 5. Browser handling

Done. `nosy browser install|path|remove`, the selection order, the minimum version, the Linux library check in `doctor`, storage capture, and the Windows Ctrl+C test. CI runs the browser tests on each system against a system Chrome and against the pinned build.

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

- Site isolation is disabled (`IsolateOrigins`, `site-per-process`, `disable-site-isolation-trials`) and `enable-unsafe-swiftshader` is on. Both were inherited from the server container: isolation off puts cross-site iframe requests on the page's own DevTools session, and software WebGL stands in for a GPU the server lacks. On a desktop, site isolation must be on unless step 6 proves capture cannot work with it. Step 6 decides, with tests for out-of-process iframes. Storage capture reads frames from the page's own session, so it needs the same test.
- The category names the index uses (`Advertising`, `Ad Motivated Tracking`, `CDN` and so on) are the old data's. Rules v1 has its own vocabulary, so `detect.ClassOf` and the CDN rule change in step 8.
- Linux arm64 and Windows arm64 have no pinned build. A system Chromium works there; whether to offer more (a Docker mode was mentioned) is for later.
- The Windows search-suggestion test (`TestOwnSearchSuggestionsAreNotScored`) failed once in CI and has passed since. If it fails again the cause is to be found, not retried.

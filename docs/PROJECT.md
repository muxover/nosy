# Nosy — maintainer notes

## What this is

Nosy is an open-source CLI that investigates what a website does to its visitors, on the user's own machine, in a clean throwaway browser. `nosy scan example.com` opens the site in an isolated Chromium, watches everything it does, checks how the site is set up, and writes a report: a terminal summary, a single-file HTML report to open or share, and a JSON file for scripts.

- Runs on the user's machine. The VPS never runs a browser again.
- Open source (GPL-3.0), so people who study privacy and security can read every line they run.
- Free, with no telemetry.
- Sharing is opt-in and ships in v0.2.0: asked on first run, default No, changeable any time.
- nosy.muxover.com becomes a thin site: install, docs, methodology, and later the public index of shared reports, dated and kept for good.

### Who it's for

People who need evidence of what a site does: privacy and security researchers; developers and site owners checking their own site before a release or in CI; auditors and agencies who need a dated report for a client; journalists who want proof they can cite.

| Tool | Good at | Misses for this job |
| --- | --- | --- |
| Browser DevTools | Seeing requests by hand | No classification, no report, slow for many sites, uses your own profile |
| Burp, ZAP, mitmproxy | Intercepting and attacking traffic | Built for pentesting; no company mapping, consent tests or score |
| EDPS Website Evidence Collector | Evidence for inspectors | Heavy setup, no history, no shared index |
| Blacklight | Quick public check | Runs on their servers, one region, no consent comparison, no CI |

Nosy's place: a clean visit, every finding with its evidence, consent and GPC tested, a CI mode, and a shared, dated public index.

## Stack and layout

- Go (current stable), one module: `github.com/muxover/nosy`. Dependencies: `chromedp` and `cdproto` for the browser, `gobwas/ws` for the second DevTools connection, `x/net` for the public suffix list.
- Three repos:

| Repo | Holds | Licence |
| --- | --- | --- |
| `muxover/nosy` (public) | The CLI | GPL-3.0 |
| `muxover/nosy-rules` (public) | Tracker domains, companies, categories, replay vendors, consent platforms, fingerprint patterns; signed releases | CC BY-SA 4.0 |
| `muxover/nosy-index` (private, v0.2.0) | Index API and the site at nosy.muxover.com | Private |

- The old implementation is in the private `muxover/nosy-legacy` repo and is read only.
- `cmd/nosy/` is the command line, on the standard library alone (`flag`, `encoding/json`): `main.go` routes, `scan.go`, `check.go`, `browser.go`, `config.go` and `doctor.go` are the commands, `print.go` the plain terminal output, `term.go` the progress line, `flags.go` the flag helpers. An `app` value holds what a command takes from the machine (streams, environment, config path), so tests run the commands in process.
- The engine lives under `internal/` and has no public API yet:
  - `internal/browser`: the Chromium driver. `browser.go` is the visit, `children.go` the second DevTools connection for frames, workers and new windows in other processes, `frames.go` and `storage.go` what is typed into and read from those frames, `lockdown.go` downloads and permissions, `profile.go` the throwaway profile and the clearing of old ones, `instrument.js` and `storage.js` the scripts that run in the page.
  - `internal/guard`: the proxy every browser byte goes through, and the dialer that checks addresses.
  - `internal/scan`: one scan, from the address to the `scan.Result`. `Build` turns a capture into a result without a browser.
  - `internal/chrome`: which browser a scan uses and Nosy's own pinned Chrome for Testing: selection, download and unpacking, library check on Linux, version check.
  - `internal/check`: the browser-less look at a site for `nosy check`.
  - `internal/site`: parsing and validating the address a user types.
  - `internal/capture`: plain data for one visit.
  - `internal/detect`: company and tracker matching, identifier cookies, fingerprinting, session recording, typed-text leaks, bot checks and thin pages.
  - `internal/score`: formula v1, unchanged from the old engine.
  - `internal/testsite`: the planted pages. `internal/integration`: the tests that visit them.
- Planned packages: `checks`, `consent`, `report`, `rules`, `publish`. On the VPS from v0.2.0: the index API takes signed uploads, validates them, re-runs the cheap setup checks and stores reports in Postgres; the site reads the same database. No browser, no queue.
- Test builds use `-tags nosytest`. That tag compiles in the guard's routing and DNS hooks, the planted-page server and a few hooks in the driver, and nothing else compiles them in. The browser tests also need `NOSY_BROWSER_TESTS=1`; without it they skip with a message. `NOSY_CHROME` names the browser, `NOSY_PARALLEL` caps the browsers open at once, `NOSY_EXPECT_PINNED=1` makes the pinned-build test run. The sandbox has no switch: where a machine forbids it, CI turns on the user namespaces it needs.
- Local folders and the CI layout follow `rules/repo.md` in `muxover/engram`.

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
- 2026-10-19: where the browser-only checks run: in the one visit, in the one browser Nosy starts on the user's machine, collected at the end of the visit and analysed afterwards by `scan.Build` from the capture. The checks that need no browser (`nosy check` now, the setup checks of step 9) run in the Go process through the guard's dialer. There is no shared browser, pool or replacement logic. `Options.Remote` stays for the isolation tests until step 6 decides. Step 6 removed it.
- 2026-10-19: the browser and everything it started are taken down when the scan's context ends: on Unix the whole process group, on Windows `taskkill /T` from the command's `Cancel` hook while the main process is still alive to name the tree. Ctrl+C on Windows is tested by starting the command with `CREATE_NEW_PROCESS_GROUP` and sending `CTRL_BREAK_EVENT`, which Go delivers as `os.Interrupt`.
- 2026-10-19: on Windows `nosy browser install` gives the build's sandboxed helper processes read access with `icacls` (ALL APPLICATION PACKAGES and ALL RESTRICTED APPLICATION PACKAGES), as Chrome's own installer does. Without it the network helper never starts from a user folder: the first call that needs it, `Network.clearBrowserCookies`, never returns and every scan times out. System Chrome is not affected. The sandbox stays on; this widens nothing the browser could not already read.
- 2026-10-19: Jax's call, step 5 answers. The `icacls` grant on Windows is fine: read-only for the same two app-package groups Chrome's own installer grants, sandbox untouched. The 8 second storage budget is fine, but a scan that hits it says so (see below). The `ci:` commits already pushed stay as they are; CI changes are `chore:` from now on. Linux and Windows on arm64 have no Chrome for Testing build, use a system Chrome, and `doctor` and `browser install` say so.
- 2026-10-19: Jax's call. History is never squashed. At v0.1.0 `build` goes to `main` with all its commits, and the release script adds one signed `chore: release v0.1.0` commit on top.
- 2026-10-19: on Windows `nosy browser install` gives the sandboxed helper processes of the build read access with `icacls` (ALL APPLICATION PACKAGES and ALL RESTRICTED APPLICATION PACKAGES), as Chrome's own installer does. From a user folder without it the network helper never starts: the first call that needs it, `Network.clearBrowserCookies`, never returns and every scan times out. System Chrome is not affected. The sandbox stays on and nothing is widened.
- 2026-10-19: the storage read has 8 seconds in all. A read that runs out of time, a database that does not open in 2 seconds, or a visit that ended on its time or size limit sets `storage_cut_short` in the conditions, adds a note, and the terminal conditions say "storage read cut short". A result never shows less storage without saying so.
- 2026-10-19: site isolation is on, as in a visitor's Chrome. The flags `IsolateOrigins`, `site-per-process` and `disable-site-isolation-trials` are gone, and a test counts the frames that arrive as targets of their own.
- 2026-10-19: a frame in another process is watched by the second DevTools connection, which already paused workers on start. Each such frame (and each frame inside it) is paused, gets the page's own setup (Network, Page, Log and Runtime on, the binding, `instrument.js` for every new document, user agent, locale, focus, touch and Global Privacy Control) and is let go. The setup is built once and replayed, so a frame in another process and the page cannot drift apart. Over its own session the same connection types the test text into the frame's fields, reads its storage in an isolated world and asks for its request bodies.
- 2026-10-19: what happens in a frame is attributed to the frame. A request carries the origin of the document that made it, a fingerprinting call or cookie write the origin it ran in, and a typed-text hit or session-recording match the frame of the request behind it. Storage was already per origin.
- 2026-10-19: `enable-unsafe-swiftshader` stays, as a fallback only. It lets Chrome fall back to software WebGL when the machine has no GPU; with a GPU Chrome uses it. Without the flag a machine with no GPU would answer "no WebGL" to scripts that check, which a visitor's Chrome would not, and fingerprinting scripts that bail out early would go unseen. Forcing software always would make results reproducible across machines but would hide what a visitor with a GPU sees, and detection does not depend on the renderer string. Each result records `webgl` as hardware, software or none, probed on the blank tab before the page loads, and lists "software WebGL (no GPU found)" in the settings when it was software. The renderer name is never kept: it identifies the user's GPU.
- 2026-10-19: each scan has a new profile in the temp folder, named `nosy-profile-` and holding a note with the owner's process number. It is deleted after a normal end, an error, Ctrl+C and a killed Chrome. A failed removal is a note on the result. The next scan clears the profiles of scans that were killed, only folders with that name and Nosy's note whose owner is gone or whose note is over an hour old (a scan lasts two minutes at most). A profile in use, a folder without the note and any other name stay. Nothing outside the temp folder is ever read or deleted, and the user's own Chrome profile, cookies and extensions are never touched.
- 2026-10-19: the browser is locked down three ways, so one failing does not open a door. Launch flags and the profile: `--deny-permission-prompts`, no extensions or component extensions with background pages, no first run or sync, content settings for every permission set to block, downloads going to a folder inside the profile. The browser connection: `Browser.setPermission` denied for every permission the protocol names and `Browser.setDownloadBehavior` deny. The page connection: every new window is closed before it loads anything and the attempt is a finding, as is every download and every address on this machine (`file:`, `chrome:` and the like) the page reaches for. Each finding comes from what the browser reports.
- 2026-10-19: `Options.Remote`, the shared browser and browser contexts are removed. The CLI never used them; only the tests did, to run many scans in one browser, and they were a second way of isolating scans that needed tests of its own. Every scan is its own browser process with its own profile, so the process boundary is the isolation, and one set of tests proves it.
- 2026-10-19: when the page script has no web address in its stack (code run from `eval`, for instance), its fingerprinting call is listed as "inline in" the frame's address.
- 2026-10-19: Jax's call, step 6 answers. The commit that does not build on its own stays: history is never rewritten. From now on every commit builds and passes vet on its own. Permissions, pop-ups, local addresses, and old profiles cleared only from the OS temp folder stay as they are.
- 2026-10-19: Jax's call. What happens in a frame is attributed to the frame's address (scheme and host of its URL), and results and reports call it that. A sandboxed frame without `allow-same-origin` has an opaque origin; its address is what is known. The planted page has one sandboxed frame without `allow-same-origin` (opaque, keeps no storage) and one with it (keeps its origin and storage), and a test checks both.
- 2026-10-19: one table of address ranges (`guard.ranges`, `net/netip`), most specific first, each entry naming its kind (loopback, private, link-local, cloud metadata, carrier-grade NAT, this network, multicast, broadcast, reserved, benchmarking, documentation, tunnel). IPv6 forms that carry an IPv4 address (mapped, compatible, NAT64 `64:ff9b::/96`, 6to4) are judged by the address they carry. A public IPv4 address behind NAT64 is allowed, because an IPv6-only network reaches every IPv4 site that way; NAT64 for local use (`64:ff9b:1::/48`), Teredo and 6to4 are refused whatever they carry. IPv4 in the spellings browsers accept (2130706433, 0177.0.0.1, 0x7f.1, 127.1) is read the WHATWG way and gets the same answer.
- 2026-10-19: the guard resolves every name, checks every answer, and dials only answers that pass, at the address it checked: a name with public and private answers only ever reaches a public one, and a name with none is refused. Names that only exist on a local network are refused before any lookup without `--allow-private`: localhost, single labels, and names ending in `.local`, `.localhost`, `.internal`, `.home.arpa` and `.lan` (the last two added: they are the home-network names routers hand out). An address refused whatever the port (a loopback address on port 8080) is named by its kind, not as a port refusal.
- 2026-10-19: cloud metadata is refused even with `--allow-private`: 169.254.169.254 and the other AWS, GCP, Azure, Alibaba and Oracle addresses, over IPv4 and IPv6. So are the guard's own port and the browser's DevTools port, which a page could otherwise use to control the browser. `--allow-private` is recorded as `allow_private` in the conditions; a report from such a scan is never shared (v0.2.0).
- 2026-10-19: the guard listens on 127.0.0.1, on a free port, for one scan, and is closed when the scan ends. Every request needs the scan's secret as proxy credentials (user `nosy`, a random 48-character password), so no other program or page can use it; the browser gets it through the DevTools connection, which holds each request for the challenge and lets it go at once (`Fetch` with `handleAuthRequests`; the browser reports a challenge only for requests it holds). A site's own password prompt gets no answer. The guard goes out directly: `HTTP_PROXY`, `HTTPS_PROXY` and system settings are ignored (`--proxy` is v0.5.0).
- 2026-10-19: Chrome is kept inside the guard by one proxy for every scheme (http, https, ws, wss), `<-loopback>` in the bypass list, no name resolution of its own (`host-resolver-rules` maps every name to nothing; async DNS, DNS over HTTPS and DNS prefetch are off, by feature, flag and profile), QUIC off, WebRTC limited to proxied UDP (of which there is none) by flag and profile, prediction, prerender and preloading off. Each way out has a planted page aimed at canaries on 127.0.0.1 and 127.0.0.2 over TCP and UDP.
- 2026-10-19: limits besides the 60 MB and the time cap: 25 MB for one answer (inside an encrypted tunnel the guard cannot see requests, so there it caps the connection), 128 connections open at once, 10 seconds for a server to start answering, 15 seconds of silence from a server the browser is waiting on. Each one that is hit is a note on the result.
- 2026-10-19: every refusal of the page's own requests is kept with the kind of address and the address of the frame that asked, in `blocked`, and shown as an observed finding: "The page tried to reach your local network". The note listing every refused destination stays, for refusals of public addresses on other ports.
- 2026-10-17: work happens one build step at a time on the `build` branch and is reviewed before the next step. Nothing is tagged or released from it.

## Status

Steps 1 to 7 of v0.1.0 are done (build order below). `nosy scan`, `check`, `browser install|path|remove`, `config`, `doctor`, `version` and `help` work, and the engine behind them is tested against a real Chromium on Linux, Windows and macOS, both a Chrome already on the machine and Nosy's pinned build, with the race detector on.

What works:

- A scan with site isolation on: frames of other sites, frames inside them and sandboxed frames are watched, and what they do is attributed to their origin.
- Requests, cookies, storage (local, session, IndexedDB, Cache Storage, service workers) per origin, fingerprinting by technique, session-recording vendors and typed-text leaks, each labelled observed, classified or inferred.
- A new profile per scan, removed after every kind of ending, with old ones from crashes cleared by the next scan.
- Downloads refused, permission prompts denied, new windows closed, addresses on the machine unreachable, each reported when the page tried.
- The guard on all three systems: no planted way out (fetch, XHR, WebSocket, images, frames, workers, service workers, redirects, meta refresh, forms, WebRTC) reaches the canaries on 127.0.0.1 and 127.0.0.2, and each attempt is named in the result with its kind and frame.
- `nosy browser install` for a pinned Chrome for Testing (155.0.8059.84) on linux64, mac-arm64, mac-x64 and win64.

What was left behind on purpose, because it is server-side or belongs to a later step: the web app, store, queue, worker, share cards, Turnstile and rate limits; the live request feed; the report renderers (step 10); technology detection from webappanalyzer rules, so consent platforms found that way are not reported, only the consent APIs the page exposes; country lookup from IP ranges (step 8).

Until rules v1 (step 8) there is no tracker data: a scan lists the companies it contacted and notes that none is classified, so scores come from fingerprinting, cookies, session recording and typed-text leaks only. Storage is reported but not scored. `scan.Result` is the type the reports will render from; its JSON layout (`nosy-scan/0`) is provisional until step 10.

Behaviour that differs from the old engine:

- The page lookups for technology rules (`window` paths and DOM selectors) are no longer collected, and `capture.Page` lost its `JS` and `DOM` fields. Page source, scripts, meta tags and response headers are still collected, for the setup checks.
- `browser.Options` lost `Probes`, `Seen` and `Remote`. `guard.StartProxy` no longer takes a listen or advertise address.
- `detect.DetectConsent` takes platform names instead of technology detections.
- The tracker classes are `detect.Class` constants with a `Class` prefix, because `detect.Replay` is a function.
- The browser starts with third-party cookie tracking protection off, and the sandbox can never be turned off.

Never tried:

- A real Windows or macOS desktop with a GPU. Every browser test ran on GitHub's runners and a Linux container, which have none or a virtual one, so hardware WebGL, high-DPI screens, several monitors and a browser that is also open for the user are untested.
- Linux or Windows on arm64. There is no pinned build there; only the error path is tested. macOS on arm64 is tested only on the runner.
- A corporate proxy, a VPN, TLS interception and a network that blocks the guard's resolver.
- An IPv6-only network that reaches IPv4 through NAT64, and a Windows machine with a Teredo tunnel up. The table handles both; neither ran.
- WebRTC with the policy off: the planted WebRTC page shows the canaries stay quiet, but there is no positive control that the page would reach them if UDP were allowed.
- A real run of `nosy browser install` outside CI, and the install channels (Homebrew, Scoop, Go, releases): step 12.
- Sites in languages and layouts beyond the planted pages and the old engine's golden list: step 11.

## How it works

These parts hold across versions.

### Scan pipeline

1. `site.Parse` checks the address; the guard refuses private, loopback, link-local and metadata addresses before any browser starts (`--allow-private` is the one way around it, with a warning on every use).
2. Old Nosy profiles from crashed scans are cleared. A new temp profile is made.
3. The guard proxy starts on a local port. Chrome starts with the profile, the proxy, site isolation on and the lockdown flags.
4. The visit: open the page, wait for a quiet network (capped), scroll like a reader, type a test string into visible text fields without sending, collect page facts, cookies and storage. The whole visit is capped by the hard stop (20 seconds by default, 10 to 120).
5. `scan.Build` turns the capture into a result: companies, cookies, storage, fingerprinting, session recording, typed text, refused items, findings, score. A thin or bot-checked first visit gets one more try. A crash or a browser network restart repeats the visit up to twice.
6. The browser is killed with its whole process tree, the profile is removed, the result is printed.

### Isolation

- One browser process and one profile per scan. Nothing is shared between scans: no cookies, storage, cache, service workers or HSTS state, and tests prove it by running two scans in a row.
- The sandbox is on and cannot be turned off. `doctor` explains the fix where a machine forbids it.
- Site isolation is on, so a frame of another site runs in its own process. A second DevTools connection attaches each such frame and each worker before its first line runs and gives it the same setup as the page (see Decisions).
- Downloads are refused, permissions denied, new windows closed, and `file:`, `chrome:` and the like unreachable. The profile and the launch flags say it, the browser connection says it, and the page connection reports what was tried.
- The user's own Chrome profile, cookies and extensions are never read.

### Guard

Every request the browser makes goes through the guard, a proxy on 127.0.0.1 that lives for one scan and answers only to the scan's secret.

- Addresses: one table of ranges in `internal/guard/addr.go`, each with its kind. Literal addresses in any spelling and every DNS answer are checked against it; IPv6 forms that carry an IPv4 address are judged by the address inside.
- Names: resolved by the guard alone, every answer checked, only a permitted answer dialed, at the address checked, and checked again on the socket. Local-network names are refused before any lookup.
- Ports: 80 and 443 only.
- `--allow-private` opens the user's own machine and network, never cloud metadata, the guard's port or the browser's DevTools port, and is written into the conditions.
- Limits: 60 MB a scan, 25 MB an answer, 128 connections, 10 seconds to start answering, 15 seconds of silence; the time cap of the visit. Each one hit is a note.
- Chrome: one proxy for every scheme, no bypass for loopback, no DNS of its own, no QUIC, no unproxied WebRTC, no prediction or prefetch.
- Results: each refused attempt of the page is in `blocked` with its kind and frame, and in an observed finding.

### Scores and labels

- Two grades, never mixed: a privacy score 0 to 100 from what the page did (public formula v1, five bands from Not nosy to Extremely nosy), and a setup grade A to F from how the site is configured (step 9).
- Every finding is labelled observed (the browser saw it happen), classified (observed, then matched to a rule or list) or inferred (concluded from a pattern), and opens to the request, cookie or script behind it and the rule it matched. Inferred findings are worded with care: reading canvas pixels does not prove a fingerprint was sent.
- Storage and refused items are observed and do not change the score.

### Conditions

Every result records its conditions: date and time, country the scan ran from (unknown until step 8), browser and version and whether it was the pinned build and why that browser was chosen, device and viewport, consent mode, Global Privacy Control, whether `--allow-private` was used, duration limit and time used, Nosy version, rules version, `webgl`, whether the storage read was cut short, and the browser settings Nosy changed (third-party cookie phase-out off, site isolation on, software WebGL). Two reports are compared only when their conditions match.

### Rules flow

- `nosy-rules` builds one versioned, signed rules file. `nosy rules update` downloads it from GitHub Releases and refuses a bad signature. The CLI ships a copy so it works offline from the first run. Every report records the rules version.
- The first job in the rules repo: map today's known tracker domains through the new sources and compare with the old results on the golden sites. Any company the new data misses gets an override with a public source.
- The CLI hands the data to every user, which counts as distributing it, so everything shipped must allow that:

| Data | Used for | Licence | Status |
| --- | --- | --- | --- |
| EasyPrivacy | Spotting tracking requests | GPLv3 or CC BY-SA 3.0; credit "The EasyList authors" | Use |
| AdGuard companiesdb | Domain to company and category | CC BY-SA 4.0 | Use |
| Nosy overrides | Fixes, replay vendors, consent platforms, fingerprint patterns | CC BY-SA 4.0 | Build |
| webappanalyzer | Technology detection | GPL-3.0 | Use |
| DB-IP Lite | Server country | CC BY 4.0, link back required | Use, credit in reports |
| HSTS preload list | Preload status | From Chromium's source | Check licence first |
| Vulnerable JS libraries | Outdated-library check | Candidate: Retire.js data | Check licence first |
| DuckDuckGo Tracker Radar | Was used before | CC BY-NC-SA 4.0 | Dropped |

### Opt-in sharing

Planned for v0.2.0 (scope below). Nothing leaves the machine unless the user turns sharing on or runs `publish`. Until then there is no sharing code. The only network use besides the scan itself is `nosy browser install`, `nosy rules update` and, on request, `nosy version --check`.

## v0.1.0: Launch

### Scope

Commands (every one works offline except `rules update` and `browser install`):

| Command | What it does | State |
| --- | --- | --- |
| `nosy scan <url>` | Full investigation plus setup checks; terminal summary, `report.html`, `report.json` | Scan and terminal summary done; setup checks step 9, reports step 10 |
| `nosy check <url>` | Setup checks only, no browser, about 2 seconds | A small version done; step 9 replaces it |
| `nosy report <file.json>` | Re-renders the HTML report from saved JSON, or opens it | Step 10 |
| `nosy doctor` | Checks browser, sandbox, rules and config, says what to fix | Done |
| `nosy config` | Shows and changes settings | Done |
| `nosy rules update` | Downloads the latest signed detection rules | Step 8 |
| `nosy browser install` | Pinned Chrome for Testing with a hash check | Done |

Scan options in v0.1.0: `--device desktop|mobile`, `--duration 20s`, `--gpc`, `--allow-private`, `--json`, `--quiet`; later `--out DIR` and `--format html,json`. Every scan types a test string into visible text fields without sending it.

What a scan analyses.

- Behaviour (browser): every request with destination, company, category, size, timing and initiating script; cookies (first and third party, before any click, identifier-like, flags; values never kept); storage per origin; fingerprinting (canvas, WebGL, audio, fonts) per script and frame; session-recording vendors; typed text leaving the page, plain or encoded; GPC; refused items.
- Setup (no browser, step 9): HTTPS (TLS version, certificate and days to expiry, redirect to HTTPS, HSTS and preload); headers (CSP and how strict it really is, frame protection, Referrer-Policy, Permissions-Policy, COOP, CORP); cookies in the first response and their flags; declared partners (`ads.txt` sellers, domains the CSP allows); page source (third-party scripts without integrity, outdated libraries with known vulnerabilities); email and domain (SPF, DMARC, MTA-STS, DNSSEC, CAA); basics (`security.txt`, a privacy policy link, server countries).
- One rule for both: only what a normal visitor's browser would do. No probing hidden paths, no attacks, no form submissions.

Outputs: the terminal summary, `report.html` (one offline file: scores, findings with evidence, who contacted whom), `report.json` (documented, versioned schema). The HTML report is safe to open: every value escaped, nothing loaded from the internet, only Nosy's own small script. Tests feed it hostile names, URLs and cookie names.

Install and distribution:

| Channel | Command | Version |
| --- | --- | --- |
| GitHub Releases | Windows, macOS, Linux (x64 and arm64), checksums and a signature | v0.1.0 |
| Go | `go install github.com/muxover/nosy/cmd/nosy@latest` | v0.1.0 |
| Homebrew | `brew install muxover/tap/nosy` | v0.1.0 |
| Scoop | `scoop bucket add muxover https://github.com/muxover/scoop-bucket` then `scoop install nosy` | v0.1.0 |
| Docker | `docker run --rm ghcr.io/muxover/nosy scan example.com` (browser included) | v0.2.0 |
| GitHub Action | `uses: muxover/nosy-action@v1` | v0.4.0 |

First run offers `nosy browser install` when no browser is found and copies the bundled rules (the sharing question joins in v0.2.0). Homebrew and Scoop avoid SmartScreen and Gatekeeper warnings, so the docs lead with them; paid code signing waits for real use.

Launch:

- A README that sells in 10 seconds: one line, an animated terminal recording of a real `nosy scan`, install commands, one screenshot of the HTML report.
- nosy.muxover.com with install, docs, methodology, privacy and terms; an example report from a real scan.
- The one launch email to the coming-soon signups, then the list is deleted. Nosy is unhidden on muxover.com with its category.
- Communities at launch: X, Telegram and GitHub "awesome" lists only. Growth later comes from research made with Nosy once consent mode exists ("I clicked Reject on 100 sites"), each number checkable by running Nosy.
- Without telemetry, use is measured by release downloads, Homebrew and Scoop counts, rules-file fetches, index submissions, stars and issues.

### Quality bar

| Kind | Proves | State |
| --- | --- | --- |
| Planted pages | Each detection fires on a page built to trigger it and stays quiet on a clean one | Running on all three systems |
| Cross-platform | Build and planted-page suite pass on Windows, macOS and Linux, on a system Chrome and the pinned build | Running |
| Isolation | Two scans in a row share no cookies, storage, cache or HSTS; the temp profile is gone after every ending; downloads, permissions, pop-ups and local addresses are refused | Done (step 6) |
| Guard | Every range in every spelling and IPv6 form (table and fuzz tests), rebinding DNS, mixed answers, local names, redirects to private hosts, every way out of a page against canaries on two local addresses, the secret, `--allow-private` | Done (step 7) |
| Rules | Tampered or unsigned rules refused; offline first run works | Step 8 |
| Hostile report input | Names, URLs and cookie names crafted to break out of the HTML report show as text | Step 10 |
| Golden sites | 20 real sites x 3 runs before release; median company count within 10% and score within 5 points of the old engine, or every difference explained | Step 11 |

CI blocks a release on everything except the golden run, which needs real sites; it runs on Jax's PC or as a manual workflow before the release, and its results go in the release notes. CI never depends on outside sites except a job whose feature is the download itself (the pinned-build jobs).

### Build order

| Step | Work | Done when | State |
| --- | --- | --- | --- |
| 1 | Save the old code to private `muxover/nosy-legacy`, recreate public `muxover/nosy` and `muxover/nosy-rules` | Both repos exist, old code safe | Done |
| 2 | Scaffold | CI green on an empty build | Done |
| 3 | Port the engine (browser, guard, capture, detect, score; server parts left behind) | Planted-page tests pass in the new repo | Done |
| 4 | CLI commands | Each prints sensible output on a test site | Done |
| 5 | Browser handling, storage capture | A clean machine goes from install to first scan with two commands | Done |
| 6 | Isolation | Isolation tests pass on all three systems | Done |
| 7 | Guard on the desktop | Guard suite passes on all three systems | Done |
| 8 | Rules v1, signed, bundled; `rules update`; DB-IP country | A tampered file is refused and offline first run works | Next |
| 9 | Setup checks | Each has fixture tests | |
| 10 | Reports: terminal, HTML, JSON schema v1, `nosy report` | Hostile-input tests pass and Jax has read a real report on his PC | |
| 11 | Golden comparison, 20 sites x 3 runs | Within tolerance, or every difference explained | |
| 12 | Packaging: binaries with checksums and a signature, Homebrew tap, Scoop bucket | Each install route works from scratch | |
| 13 | Docs site replaces coming-soon | Passes the website rules at phone and desktop width | |
| 14 | Release and launch: the release script adds one signed commit on top of the full history and tags v0.1.0; launch email; signup list deleted; Nosy unhidden on muxover.com | A fresh install from each channel scans a real site and the email has gone out | |

Progress: 7 of 14 steps done.

### Release blockers

- A scan that touches the user's own profile or network.
- A report that runs hostile input.
- A tampered rules file accepted.
- A finding the evidence doesn't back.
- A leftover profile or Chrome process.
- CI red on any system.
- Golden sites outside tolerance, unexplained.

## v0.2.0: Index

Scope:

- `nosy publish <file.json>`, sent only with sharing on or `--share`; the first-run question (default No, saved in config, overridden per scan with `--share` and `--no-share`); the terminal always says when a report was shared, with its link.
- Only public sites (the server checks the host resolves to public addresses), and never a scan made with `--allow-private` (its conditions say so). The report JSON only, never a HAR. No IPs kept (country derived, then the IP dropped), no local paths, cookie values, typed test words or install name. Each install has a random key that signs uploads.
- Trust: every report shows "shared by a Nosy user" with date, country and versions. The server validates the schema, the evidence against the findings and the rules version, and re-runs the cheap setup checks. Agreement across scans builds confidence; outliers wait for review. Site owners can ask for a correction under a published policy.
- The index site: search a site, history over time and by country, compare dates, methodology, open rules. The Docker image.
- Quality bar: upload validation and privacy tests pass (bad signature, replay, oversized or inconsistent reports, private hosts, rate limits); the index terms, privacy and correction policy are live.
- Before the index goes public, a lawyer answers two questions: the UAE online defamation risk of dated user reports about named sites, and the privacy terms the index needs.

## Later

| Version | Theme | Scope |
| --- | --- | --- |
| v0.3.0 | Consent | `--consent accept/reject/all`, `--gpc` comparison, `nosy diff`, side by side in the HTML report. Must be true: consent tests on planted banners from the common platforms. |
| v0.4.0 | CI | `nosy ci` with `--max-score` and `--no-new-trackers`, exit codes and a Markdown summary, the GitHub Action. Must be true: the action fails a pull request on a sample repo when a new tracker appears. |
| v0.5.0 | Deeper visits | `--pages N` on the same site, `--proxy`, more on storage and service workers. Must be true: crawling never leaves the site and respects its limit. |
| v0.6.0 | Security depth | CSP analysis, integrity checks, vulnerable libraries, technology stack, email and domain checks. Must be true: each check has fixtures and the data licences are confirmed. |
| v0.7.0 | Research scale | `nosy batch` with a polite delay, HAR export, network graph and timeline. Must be true: 100 sites in a batch without leaks between scans. |
| v0.8.0 | Trust | `--sandbox docker`, agreement across submissions, review queue for outliers. Must be true: container mode passes the safety suite. |
| v0.9.0 | Hardening | Outside security review, fuzzing, full docs, schema freeze proposal. Must be true: no open high-severity issues. |
| v1.0.0 | Stable | JSON schema and CLI flags frozen, signing keys documented. Must be true: 14 days at v0.9.x with no incident. |

After v1.0.0, only if wanted: Nosy Cloud (paid, through pay.muxover.com), with scheduled monitoring and alerts, private history, signed audit PDFs and team workspaces. Not before the free tool has steady users asking for it.

Never: telemetry, scanning on Nosy's servers, probing hidden paths, a browser without its sandbox.

## Open questions

- The category names the index uses (`Advertising`, `Ad Motivated Tracking`, `CDN` and so on) are the old data's. Rules v1 has its own vocabulary, so `detect.ClassOf` and the CDN rule change. Settled in step 8.
- Whether `nosy check` stays a separate command once the setup checks exist, or becomes `nosy scan --no-browser`. Settled in step 9.
- Whether to offer a Docker mode as the safe route on machines that forbid the sandbox, and so on arm64 Linux. Settled in v0.8.0 (`--sandbox docker`); until then `doctor` explains the fix.
- Whether the golden list of 20 sites needs sites from more languages and regions than the old engine's. Settled in step 11.
- Whether a Windows or macOS desktop with a GPU shows differences in fingerprint detection that the runners hide. Settled in step 10, when Jax reads a real report on his PC.

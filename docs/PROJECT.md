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

- Go (current stable), one module: `github.com/muxover/nosy`.
- `cmd/nosy/` holds the entry point. The engine (browser driver, guard, capture, detect, score) arrives in step 3 and is ported from the old code, not copied wholesale.
- Old code lives in the private `muxover/nosy-legacy` repo and is read only.
- Rules live in `muxover/nosy-rules`.

## Decisions

- 2026-10-17: licence is GPL-3.0.
- 2026-10-17: the new engine matches the old one on golden sites when the median company count is within 10% and the score is within 5 points.
- 2026-10-17: the coming-soon page stays up until v0.1.0.
- 2026-10-17: the public index is v0.2.0 scope. v0.1.0 has no sharing code.
- 2026-10-17: work happens one build step at a time on the `build` branch and is reviewed before the next step. Nothing is tagged or released from it.

## Status

Step 2 (scaffold) is in place: module, `nosy version` and `nosy help`, README, CHANGELOG, licence, security policy, lint config and CI. There is no engine yet.

## Next

Step 3: port the engine from the old code.

## v0.1.0 build order

### 1. Save old code

Done. The previous implementation is kept in `muxover/nosy-legacy`.

### 2. Scaffold

Module, entry point with `version` and `help`, repo files, CI and release workflow, repo settings.

### 3. Port engine

Browser driver, guard, capture, detect and score, ported from the old code.

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

None.

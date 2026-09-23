# AGENT HANDOFF — wankarr (read this first in a fresh session)

Wankarr: sidecar Go app next to XBVR. Local VR-tracker index (RSS feeds +
explicit Jackett/Torznab searches) → variant grouping by quality →
wishlist matching → one-click send to Transmission → post-download link
back into the XBVR scene. UI: dependency-free page served by the binary.

## Identity (critical)

- GitHub actor is **moToroTor**. NEVER author as tomck. Repo pins
  `user.name=moToroTor / user.email=157328902+moToroTor@users.noreply.github.com` — verify with `git config user.email`.
- `gh` flips active account to tomck between sessions. Check
  `gh api user --jq .login`; if tomck, `gh auth switch --user moToroTor`.
- Repo: `github.com/moToroTor/wankarr`, **PUBLIC**, description
  "Turning your XBVR Wishlist into an XBVR Achieved list".
- Local: `/Users/tom/xbvr/wankarr`, branch `main`, in sync with origin.

## Commit history (all moToroTor-authored, pushed)

- `5b844fa` Initial Wankarr sidecar for XBVR wishlist matching
- `b67d33c` Flag HBR from title tokens, add Find-versions button
- `8f0e9b2` Link finished wishlist grabs into their XBVR scene
  (serveSend records pending_links row with Transmission ID; 5-min
  poller watches torrent-get → XBVR rescan on completion → files/match
  fallback; torrent ID/status added to client, rescan+find+match to XBVR
  client)

## Layout

- Repo root: `/Users/tom/xbvr/wankarr` (`main.go`, `web/`, `internal/*/`, `README.md`, `LICENSE.md` GPL-3.0, `.env.example`).
- XBVR source (read-only reference): `/Users/tom/xbvr/xbapps/xbvr`
  (moToroTor fork of xbapps/xbvr; wishlist auto-clears — see below).
- Untracked local files (NEVER commit): `.env` (real secrets),
  `wankarr.db` (runtime data), `wankarr` binary, `session-ses_f49a.md`
  (OpenCode transcript, secret-free but bulky — delete when done?).
- Scratch clones in /tmp (Whisparr, Prowlarr, Jackett): reference only.

## Key facts (verified, don't re-derive)

- XBVR clears wishlist itself: `UpdateStatus` sets `IsAvailable=true` AND
  `Wishlist=false` when the first video file lands — anything Wankarr does
  on fulfillment is belt-and-braces, not load-bearing.
- Tracker etiquette is load-bearing: NO API use, NO crawling, NO auto-grab.
  Emp traffic = scheduled RSS polls (conditional GET) + explicit
  user-triggered Jackett searches only.
- Torznab has NO tags element: Emp tag vocab arrives inside
  `<description>` ("Verified:/Unverified: ...") via Cardigann mapping.
  RSS sizes live nested in `<torrent><contentLength>` (flat mapping
  silently yields 0 — bitten once already).
- Resolution map includes odd classes: 2700p (CzechVR 6K), 1600p (GearVR).
  HBR = `high.bitrate` tag OR title tokens (HQ/HBR); codec badges h265/h264/av1.
- VR filter: token-based IsVR + notVRPhrases exclusions
  ("vr to normal", "vr2normal", ...). Groups view VR-only by default.
- Secrets: agent NEVER opens `.env`; fixtures use example.invalid;
  enclosure URLs (authkey/torrent_pass) never logged/rendered —
  `store.EnclosureURL` dedicated lookup only.

## Workflow conventions

- `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .` clean
  before finishing. Regression test per behavior fix (fixtures in-package).
- UI work: plain dark tables, escape all feed strings (XSS), `node --check`.
- Don't start/stop the user's running instance or Firefox; use a free
  port (HTTP_PORT) for smoke tests, terminate afterward.
- gh auth switch needs explicit user approval per session norms; commit +
  push need explicit "commit and push" (git skill loaded for history writes).

## In flight (as of 2026-09-22)

- Transmission 401: RESOLVED — torrent push confirmed working by user.
- Uncommitted work: group-merge fix (mergeSimilar in variant.go fuses
  reordered-performer title buckets at Jaccard ≥0.8, ≥4 shared tokens;
  tests TestGroupMergesReorderedPerformerTitles +
  TestGroupKeepsDifferentScenesApart). Full suite green, needs
  commit + push with explicit user approval.

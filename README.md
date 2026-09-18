# Wankarr

Companion sidecar for [XBVR](https://github.com/xbapps/xbvr). It keeps a
local index of private-tracker uploads, groups duplicate uploads of the
same scene by quality (resolution, codec, HBR vs standard), matches them
against your XBVR library and wishlist, and queues chosen torrents in
Transmission.

## Tracker etiquette (read this)

Private trackers commonly forbid automation and unsupported API use, and
may disable accounts making unreasonable automated requests. Wankarr is
designed accordingly:

- The standing index comes from **your own RSS notification feeds**,
  polled every few hours with conditional GETs. Default: every 4h
  (`POLL_INTERVAL`). Feed URLs carry their own auth; list as many as
  you like.
- **Jackett/Torznab is for explicit searches only** — one request per
  configured indexer per search you trigger in the UI. Wankarr never
  crawls, polls Jackett on a schedule, or auto-grabs anything.
- Every send to Transmission is one click by you, one torrent at a time.

## Setup

1. Copy `.env.example` to `.env` and fill in your values (never commit
   `.env` — see `.gitignore`):
   - `XBVR_URL` — your XBVR instance.
   - `RSS_FEEDS` — private notification feed URL(s), comma-separated.
   - `JACKETT_URL`, `JACKETT_API_KEY`, `JACKETT_TORZNAB_PATHS` —
     local Jackett with your Torznab indexer path(s).
   - `TRANSMISSION_URL`, `TRANSMISSION_USER`, `TRANSMISSION_PASS`.
   - `TRANSMISSION_DOWNLOAD_DIR` (optional) — overrides Transmission's
     default download directory for Wankarr sends. Empty = server default.
2. `./wankarr doctor` — checks config and connectivity (XBVR, Jackett,
   Transmission, feeds) and exits nonzero with a FAIL line per problem.
3. `go build -o wankarr . && ./wankarr`
4. Open http://127.0.0.1:8060/ (`HTTP_PORT` to change).

## How it works

- **Ingest**: RSS polls and Jackett searches become `emp_items` rows keyed
  by Emp torrent-group ID (dedupe is automatic).
- **Grouping**: titles minus the trailing variant parenthetical form a
  scene group; resolution comes from tags (`4096p`, `3840p`, …) with title
  fallback (`Oculus 8K`); `high.bitrate` marks HBR variants. Funscripts,
  packs, and other accessories are excluded from scene rows.
- **Jackett records** also carry seeders and freeleech status, and the
  Emp tag list (parsed from the result description, which is how
  Cardigann exposes it). The Groups view is VR-only by default
  (`?vr=0` shows everything).
- **Matching**: fresh items are scored against the XBVR wishlist
  (title-token overlap + studio bonus, threshold 0.6) into
  `wishlist_matches` — the New matches view.
- **Send**: `POST /api/send {"group_id": …}` looks up the
  credential-bearing enclosure URL server-side and calls Transmission
  `torrent-add` (with `download-dir` when `TRANSMISSION_DOWNLOAD_DIR` is
  set). Only the group ID is logged.

## Secret hygiene

Tracker enclosure URLs embed your personal credentials (`authkey`,
`torrent_pass`, passkeys). Wankarr never logs or renders them: bulk
queries exclude the column, the UI only ever sends group IDs, and logs
redact feed URLs. Test fixtures must use scrubbed samples (fake host, no
credentials) — see `internal/emp` and `internal/jackett` tests.

## Development

- `go test ./...`, `go vet ./...`, `gofmt -l .` (must be clean).
- No cgo: SQLite via `modernc.org/sqlite`. UI is dependency-free
  vanilla JS in `web/` (embedded in the binary).

## License

GPL-3.0 (see `LICENSE.md`), matching the *arr projects. Note that XBVR
itself ships no license; Wankarr is an independent sidecar.

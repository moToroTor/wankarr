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
  fallback (`Oculus 8K`); `high.bitrate` marks HBR variants, with title
  tokens (`HBR`, `HQ`, `high bitrate`) as fallback for tag-less Jackett
  records. Funscripts,
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
- **Link-back**: when a send matches a wishlist scene, Wankarr registers
  the torrent's inner video filenames on the scene's known-filenames
  list (`scene/filenames/{id}` append endpoint), so XBVR's next
  library scan auto-matches them —
  even when the downloaded names differ from the scraped release names.
  Season-pack-sized torrents are skipped (episode names must not land on
  one scene). Sends are also tracked; every 5 minutes Wankarr polls
  Transmission, and once the torrent finishes it calls XBVR's `rescan`
  task and links the new file to the scene (`files/match` when
  auto-match misses) as a fallback. Non-wishlist sends stay
  fire-and-forget. Point `TRANSMISSION_DOWNLOAD_DIR` inside a path XBVR
  watches, or the rescan will never see the file.

## Secret hygiene

Tracker enclosure URLs embed your personal credentials (`authkey`,
`torrent_pass`, passkeys). Wankarr never logs or renders them: bulk
queries exclude the column, the UI only ever sends group IDs, and logs
redact feed URLs. Test fixtures must use scrubbed samples (fake host, no
credentials) — see `internal/emp` and `internal/jackett` tests.

## Running on a Synology NAS (DSM 7)

Wankarr ships as a proper DSM 7 package: install it from Package Center
and it runs as its own service user, survives reboots, and notifies you
of updates.

**First install:** download the `.spk` for your arch from the
[releases page](https://github.com/moToroTor/wankarr/releases), then
Package Center → Manual Install. DSM warns about the third-party
publisher (normal — signing has no effect on DSM 7). The install wizard
asks for your XBVR/Transmission/tracker settings (passwords included) and
writes `/var/packages/wankarr/var/.env` for you — upgrades show the same
pages pre-filled from that file, and never overwrite it behind your back.
The service runs the binary with that directory as its working directory,
so `wankarr.db` lives next to `.env`. The UI listens on all interfaces
(`HTTP_HOST` to pin it down, `HTTP_PORT` to move it), so DSM's app icon
and any LAN browser reach it.

**Auto-updates:** add the catalog URL as a Package Source (Package
Center → Settings → Package Sources). Each tagged release rebuilds the
`.spk` files and the catalog, so new versions appear as Update badges:

`https://moToroTor.github.io/wankarr/api/package`

**Command-line upgrades:** Package Center has no catalog refresh button,
so for the newest build without waiting on the UI, run
[`update.sh`](update.sh) over SSH — it picks the newest `.spk` for your
arch from the catalog and installs it with `synopkg` (the same in-place
upgrade Package Center performs, data preserved):

```sh
curl -fsSL https://raw.githubusercontent.com/moToroTor/wankarr/main/update.sh -o /tmp/update.sh
sudo sh /tmp/update.sh          # or: sh /tmp/update.sh --check
```

**NAS `.env` notes:** Transmission and Jackett are local, so
`TRANSMISSION_URL=http://127.0.0.1:9091/transmission/rpc` and
`JACKETT_URL=http://127.0.0.1:9117`; `XBVR_URL` stays whatever it is on
the LAN.

**Manual alternative (no package):** `make nas`, copy
`dist/wankarr-linux-amd64` plus a NAS `.env` anywhere sensible (not the
docker folder), and autostart via DSM Task Scheduler (Triggered Task →
Boot-up): `cd /path/to/wankarr && ./wankarr-linux-amd64 >> wankarr.log 2>&1`.

## Cutting a release (maintainer)

1. `git tag vX.Y.Z && git push origin vX.Y.Z`
2. The `release` workflow cross-compiles (x86-64/avoton, ARM64),
   assembles per-arch `.spk` files, publishes the GitHub Release with
   assets, and redeploys the Package Center catalog to GitHub Pages.
3. Test the update path on the NAS before announcing anything.

## Development

- `go test ./...`, `go vet ./...`, `gofmt -l .` (must be clean).
- No cgo: SQLite via `modernc.org/sqlite`. UI is dependency-free
  vanilla JS in `web/` (embedded in the binary).

## License

GPL-3.0 (see `LICENSE.md`), matching the *arr projects. Note that XBVR
itself ships no license; Wankarr is an independent sidecar.

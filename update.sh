#!/bin/sh
# update.sh — upgrade Wankarr on Synology DSM 7 from the command line.
#
#   sudo sh update.sh          # install/upgrade to the newest catalog build
#   sh update.sh --check       # show installed vs catalog version, change nothing
#
# Resolves the newest .spk for this NAS from the Pages catalog and hands
# it to synopkg, which upgrades in place: /var/packages/wankarr/var/.env
# and wankarr.db are preserved (fresh-install rules only apply when the
# package is absent). Installing requires root; --check does not.
set -eu

CATALOG_URL="https://motorotor.github.io/wankarr/catalog.json"
PKG="wankarr"

case "$(uname -m)" in
  x86_64) ARCH=avoton ;;
  aarch64|arm64) ARCH=aarch64 ;;
  *) echo "update: unsupported arch '$(uname -m)'" >&2; exit 1 ;;
esac

INSTALLED="none"
if [ -f "/var/packages/${PKG}/INFO" ]; then
  INSTALLED=$(grep '^version=' "/var/packages/${PKG}/INFO" | cut -d'"' -f2)
fi

LINK=$(curl -fsSL "$CATALOG_URL" | grep -o 'https://[^"]*'"${ARCH}"'[^"]*\.spk' | head -n 1)
if [ -z "$LINK" ]; then
  echo "update: no ${ARCH} package found in catalog" >&2
  exit 1
fi
# Asset names look like wankarr_avoton-7.1_0.1.22-1.spk while the
# installed INFO carries the same trailing version-rev, so the two
# compare directly.
AVAILABLE=$(basename "$LINK" .spk | grep -o '[0-9][0-9.]*-[0-9][0-9]*$')

echo "installed: ${INSTALLED}  available: ${AVAILABLE} (${ARCH})"
if [ "$INSTALLED" = "$AVAILABLE" ]; then
  echo "already up to date"
  exit 0
fi

if [ "${1:-}" = "--check" ]; then
  exit 0
fi

if [ "$(id -u)" -ne 0 ]; then
  echo "update: run as root to install (e.g. sudo sh update.sh)" >&2
  exit 1
fi

SPK="/tmp/wankarr-${ARCH}.spk"
trap 'rm -f "$SPK"' EXIT INT TERM
curl -fsSL -o "$SPK" "$LINK"
synopkg install "$SPK"
echo "now installed: $(grep '^version=' "/var/packages/${PKG}/INFO" | cut -d'"' -f2)"

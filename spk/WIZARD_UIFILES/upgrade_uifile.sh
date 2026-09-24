#!/bin/bash
# Wankarr upgrade wizard. DSM executes this and reads the wizard JSON it
# writes to ${SYNOPKG_TEMP_LOGFILE}. Every box is pre-filled from the
# current ${SYNOPKG_PKGVAR}/.env so changing one setting is a small edit;
# submitting writes the shown values back (see service_postupgrade).
#
# Pages are built by concatenating single-quoted literals with
# double-quoted values: concatenation passes values through byte-for-byte
# on every bash (unlike ${//} substitution, which collapses backslashes
# on bash 4+). quote_json makes values JSON-safe first.

quote_json()
{
    printf '%s' "$1" | sed -e 's|\\|\\\\|g' -e 's|"|\\"|g'
}

WIZARD_ENV_FILE="${SYNOPKG_PKGVAR:-}/.env"

env_get()
{
    # $1 = KEY. Prints the decoded value from the current .env (the file
    # service_postinst writes: KEY='value' with '\'' for quotes).
    line=$(grep -E "^$1=" "$WIZARD_ENV_FILE" 2>/dev/null | head -n 1) || return 0
    [ -n "$line" ] || return 0
    raw=${line#*=}
    case $raw in
        \'*\') ;;
        *) printf '%s' "$raw"; return 0 ;;
    esac
    inner=${raw#\'}; inner=${inner%\'}
    # Decode '\'' escapes with sed: bash ${//} mishandles quoted
    # backslash patterns on older bash (e.g. macOS 3.2).
    printf '%s' "$inner" | sed "s/'\\\\''/'/g"
}

with_default()
{
    # $1 = KEY, $2 = fallback. Prints the current .env value or fallback.
    val=$(env_get "$1")
    if [ -z "$val" ]; then
        printf '%s' "$2"
    else
        printf '%s' "$val"
    fi
}

XBVR_URL=$(quote_json "$(with_default XBVR_URL 'http://127.0.0.1:9999')")
TR_URL=$(quote_json "$(with_default TRANSMISSION_URL 'http://127.0.0.1:9091/transmission/rpc')")
TR_USER=$(quote_json "$(with_default TRANSMISSION_USER '')")
TR_PASS=$(quote_json "$(with_default TRANSMISSION_PASS '')")
TR_DIR=$(quote_json "$(with_default TRANSMISSION_DOWNLOAD_DIR '')")
RSS_FEEDS=$(quote_json "$(with_default RSS_FEEDS '')")
JK_URL=$(quote_json "$(with_default JACKETT_URL 'http://127.0.0.1:9117')")
JK_KEY=$(quote_json "$(with_default JACKETT_API_KEY '')")
TORZNAB=$(quote_json "$(with_default JACKETT_TORZNAB_PATHS '/api/v2.0/indexers/empornium/results/torznab/')")

PAGE_MEDIA='{
    "step_title": "Media servers (current values shown)",
    "items": [{
        "type": "textfield",
        "desc": "Where Wankarr reads your library and sends downloads.",
        "subitems": [{
            "key": "wizard_xbvr_url",
            "desc": "XBVR URL",
            "defaultValue": "'"$XBVR_URL"'",
            "validator": {"allowBlank": false}
        }]
    }, {
        "type": "textfield",
        "desc": "Transmission RPC endpoint (runs on this NAS).",
        "subitems": [{
            "key": "wizard_transmission_url",
            "desc": "Transmission URL",
            "defaultValue": "'"$TR_URL"'",
            "validator": {"allowBlank": false}
        }]
    }, {
        "type": "textfield",
        "desc": "Leave user and password empty if Transmission has no auth configured.",
        "subitems": [{
            "key": "wizard_transmission_user",
            "desc": "Transmission user (optional)",
            "defaultValue": "'"$TR_USER"'"
        }]
    }, {
        "type": "password",
        "desc": "Stored only in the package'\''s private .env file.",
        "subitems": [{
            "key": "wizard_transmission_pass",
            "desc": "Transmission password (optional)",
            "defaultValue": "'"$TR_PASS"'"
        }]
    }, {
        "type": "textfield",
        "desc": "Must exist and be writable by Transmission. Empty = server default.",
        "subitems": [{
            "key": "wizard_transmission_dir",
            "desc": "Download directory override (optional)",
            "defaultValue": "'"$TR_DIR"'"
        }]
    }]
}'

PAGE_TRACKER='{
    "step_title": "Tracker access (current values shown)",
    "items": [{
        "type": "password",
        "desc": "Private RSS notification feed URL(s) from the tracker. Comma-separated if more than one.",
        "subitems": [{
            "key": "wizard_rss_feeds",
            "desc": "RSS feed URL(s)",
            "defaultValue": "'"$RSS_FEEDS"'",
            "validator": {"allowBlank": false}
        }]
    }, {
        "type": "textfield",
        "desc": "Jackett server for on-demand searches (runs on this NAS). Optional.",
        "subitems": [{
            "key": "wizard_jackett_url",
            "desc": "Jackett URL (optional)",
            "defaultValue": "'"$JK_URL"'"
        }]
    }, {
        "type": "password",
        "desc": "From the Jackett dashboard, top-right. Optional.",
        "subitems": [{
            "key": "wizard_jackett_api_key",
            "desc": "Jackett API key (optional)",
            "defaultValue": "'"$JK_KEY"'"
        }]
    }, {
        "type": "textfield",
        "desc": "Comma-separated if more than one. Per indexer: Jackett dashboard, indexer, Torznab feed.",
        "subitems": [{
            "key": "wizard_torznab_paths",
            "desc": "Torznab indexer path(s)",
            "defaultValue": "'"$TORZNAB"'"
        }]
    }]
}'

main()
{
    printf '[%s,%s]\n' "$PAGE_MEDIA" "$PAGE_TRACKER" > "${SYNOPKG_TEMP_LOGFILE}"
}

main "$@"

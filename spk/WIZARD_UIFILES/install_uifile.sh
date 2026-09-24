#!/bin/bash
# Wankarr install wizard (dynamic uifile generator, cf. SynoCommunity
# transmission's install_uifile.sh). DSM executes this and reads the
# wizard JSON it writes to ${SYNOPKG_TEMP_LOGFILE}. Fresh installs have
# no .env yet, so defaults are the built-in ones.
#
# Pages are built by concatenating single-quoted literals with
# double-quoted values: concatenation passes values through byte-for-byte
# on every bash (unlike ${//} substitution, which collapses backslashes
# on bash 4+). quote_json makes values JSON-safe first.

quote_json()
{
    printf '%s' "$1" | sed -e 's|\\|\\\\|g' -e 's|"|\\"|g'
}

XBVR_URL=$(quote_json "${wizard_xbvr_url:-http://127.0.0.1:9999}")
TR_URL=$(quote_json "${wizard_transmission_url:-http://127.0.0.1:9091/transmission/rpc}")
TR_USER=$(quote_json "${wizard_transmission_user:-}")
TR_PASS=$(quote_json "${wizard_transmission_pass:-}")
TR_DIR=$(quote_json "${wizard_transmission_dir:-}")
RSS_FEEDS=$(quote_json "${wizard_rss_feeds:-}")
JK_URL=$(quote_json "${wizard_jackett_url:-http://127.0.0.1:9117}")
JK_KEY=$(quote_json "${wizard_jackett_api_key:-}")
TORZNAB=$(quote_json "${wizard_torznab_paths:-/api/v2.0/indexers/empornium/results/torznab/}")

PAGE_MEDIA='{
    "step_title": "Media servers",
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
    "step_title": "Tracker access",
    "items": [{
        "type": "password",
        "desc": "Private RSS notification feed URL(s) from the tracker. Comma-separated if more than one. The URL itself carries the auth, so it is asked as a password.",
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

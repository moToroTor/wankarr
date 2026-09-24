#!/bin/bash
# Wankarr install wizard (dynamic uifile generator, cf. SynoCommunity
# transmission's install_uifile.sh). DSM executes this and reads the
# wizard JSON it writes to ${SYNOPKG_TEMP_LOGFILE}. Fresh installs have
# no .env yet, so defaults are the built-in ones.
#
# Values are substituted via bash replacement (not heredoc expansion)
# so JSON escaping survives verbatim.

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

PAGE_MEDIA=$(/bin/cat<<'EOF'
{
    "step_title": "Media servers",
    "items": [{
        "type": "textfield",
        "desc": "Where Wankarr reads your library and sends downloads.",
        "subitems": [{
            "key": "wizard_xbvr_url",
            "desc": "XBVR URL",
            "defaultValue": "__XBVR_URL__",
            "validator": {"allowBlank": false}
        }]
    }, {
        "type": "textfield",
        "desc": "Transmission RPC endpoint (runs on this NAS).",
        "subitems": [{
            "key": "wizard_transmission_url",
            "desc": "Transmission URL",
            "defaultValue": "__TR_URL__",
            "validator": {"allowBlank": false}
        }]
    }, {
        "type": "textfield",
        "desc": "Leave user and password empty if Transmission has no auth configured.",
        "subitems": [{
            "key": "wizard_transmission_user",
            "desc": "Transmission user (optional)",
            "defaultValue": "__TR_USER__"
        }]
    }, {
        "type": "password",
        "desc": "Stored only in the package's private .env file.",
        "subitems": [{
            "key": "wizard_transmission_pass",
            "desc": "Transmission password (optional)",
            "defaultValue": "__TR_PASS__"
        }]
    }, {
        "type": "textfield",
        "desc": "Must exist and be writable by Transmission. Empty = server default.",
        "subitems": [{
            "key": "wizard_transmission_dir",
            "desc": "Download directory override (optional)",
            "defaultValue": "__TR_DIR__"
        }]
    }]
}
EOF
)

PAGE_TRACKER=$(/bin/cat<<'EOF'
{
    "step_title": "Tracker access",
    "items": [{
        "type": "password",
        "desc": "Private RSS notification feed URL(s) from the tracker. Comma-separated if more than one. The URL itself carries the auth, so it is asked as a password.",
        "subitems": [{
            "key": "wizard_rss_feeds",
            "desc": "RSS feed URL(s)",
            "defaultValue": "__RSS_FEEDS__",
            "validator": {"allowBlank": false}
        }]
    }, {
        "type": "textfield",
        "desc": "Jackett server for on-demand searches (runs on this NAS). Optional.",
        "subitems": [{
            "key": "wizard_jackett_url",
            "desc": "Jackett URL (optional)",
            "defaultValue": "__JK_URL__"
        }]
    }, {
        "type": "password",
        "desc": "From the Jackett dashboard, top-right. Optional.",
        "subitems": [{
            "key": "wizard_jackett_api_key",
            "desc": "Jackett API key (optional)",
            "defaultValue": "__JK_KEY__"
        }]
    }, {
        "type": "textfield",
        "desc": "Comma-separated if more than one. Per indexer: Jackett dashboard, indexer, Torznab feed.",
        "subitems": [{
            "key": "wizard_torznab_paths",
            "desc": "Torznab indexer path(s)",
            "defaultValue": "__TORZNAB__"
        }]
    }]
}
EOF
)

main()
{
    PAGE_MEDIA=${PAGE_MEDIA//__XBVR_URL__/$XBVR_URL}
    PAGE_MEDIA=${PAGE_MEDIA//__TR_URL__/$TR_URL}
    PAGE_MEDIA=${PAGE_MEDIA//__TR_USER__/$TR_USER}
    PAGE_MEDIA=${PAGE_MEDIA//__TR_PASS__/$TR_PASS}
    PAGE_MEDIA=${PAGE_MEDIA//__TR_DIR__/$TR_DIR}
    PAGE_TRACKER=${PAGE_TRACKER//__RSS_FEEDS__/$RSS_FEEDS}
    PAGE_TRACKER=${PAGE_TRACKER//__JK_URL__/$JK_URL}
    PAGE_TRACKER=${PAGE_TRACKER//__JK_KEY__/$JK_KEY}
    PAGE_TRACKER=${PAGE_TRACKER//__TORZNAB__/$TORZNAB}
    echo "[$PAGE_MEDIA,$PAGE_TRACKER]" > "${SYNOPKG_TEMP_LOGFILE}"
}

main "$@"

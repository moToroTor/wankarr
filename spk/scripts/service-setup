#!/bin/sh
# Wankarr service definition (follows spksrc generic service conventions).

WANKARR="${SYNOPKG_PKGDEST}/bin/wankarr"
SERVICE_COMMAND="${WANKARR}"
SVC_BACKGROUND=y
SVC_WRITE_PID=y
PID_FILE="${SYNOPKG_PKGVAR}/wankarr.pid"
LOG_FILE="${SYNOPKG_PKGVAR}/wankarr.log"

# Run with the var dir as cwd: .env and wankarr.db live there.
SVC_CWD="${SYNOPKG_PKGVAR}"

service_postinst()
{
    # Seed a starter .env on fresh install; never overwrite an existing one
    # (it holds the user's tracker credentials).
    if [ ! -f "${SYNOPKG_PKGVAR}/.env" ]; then
        cp "${SYNOPKG_PKGDEST}/share/wankarr/dot-env-example" "${SYNOPKG_PKGVAR}/.env"
        chmod 600 "${SYNOPKG_PKGVAR}/.env"
    fi
}

service_prestart()
{
    # Ensure the var dir exists for .env, wankarr.db, pid and log files.
    mkdir -p "${SYNOPKG_PKGVAR}"
}

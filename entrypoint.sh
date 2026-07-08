#!/bin/sh
set -e

# Use environment variables PUID/PGID, otherwise default to 10001
PUID=${PUID:-10001}
PGID=${PGID:-10001}

# Validate PUID/PGID
if [ "${PUID}" -lt 1 ] || [ "${PGID}" -lt 1 ]; then
    echo "ERROR: PUID and PGID must non-root (0) and positive integers (got PUID=${PUID}, PGID=${PGID})"
    exit 1
fi

# Create group/user entries when the requested IDs are not taken yet.
# A GID/UID that already exists (e.g. Alpine ships group "users" with GID 100,
# which Unraid requires as PGID, see #995) is simply reused — privileges are
# dropped by numeric ID below, so a pre-existing entry with a different name
# works just as well. Creation is best-effort: a name clash from a previous
# container start with different IDs must not prevent startup either.
if ! getent group "${PGID}" >/dev/null 2>&1; then
    addgroup -g "${PGID}" paperless-gpt \
        || echo "WARN: could not create group paperless-gpt (GID ${PGID}); continuing with numeric GID"
fi

if ! getent passwd "${PUID}" >/dev/null 2>&1; then
    GROUP_NAME=$(getent group "${PGID}" | cut -d: -f1)
    adduser -D -S -h /home/paperless-gpt -s /sbin/nologin -G "${GROUP_NAME:-nogroup}" -u "${PUID}" paperless-gpt \
        || echo "WARN: could not create user paperless-gpt (UID ${PUID}); continuing with numeric UID"
fi

# Create necessary directories
mkdir -p /app/prompts /app/config /app/db /home/paperless-gpt

# Set ownership for app and home directories to handle all file permissions
chown -R "${PUID}:${PGID}" /app /home/paperless-gpt

# Drop privileges and execute the main application. Numeric uid:gid makes
# su-exec apply exactly the requested IDs, independent of which passwd/group
# entry (if any) they resolve to. HOME is set after the drop (via env) because
# su-exec overwrites it with the home dir of whatever passwd entry the UID
# resolves to (e.g. "/" for a pre-existing nobody/65534).
echo "Starting application as ${PUID}:${PGID}"
exec su-exec "${PUID}:${PGID}" env HOME=/home/paperless-gpt /app/paperless-gpt

#!/bin/sh
set -e

# Use environment variables PUID/PGID, otherwise default to 1000
PUID=${PUID:-1000}
PGID=${PGID:-1000}

# Create group and user
if ! getent group paperless-gpt >/dev/null; then
    addgroup -g ${PGID} paperless-gpt
fi

if ! getent passwd paperless-gpt >/dev/null; then
    adduser -D -S -h /home/paperless-gpt -s /sbin/nologin -G paperless-gpt -u ${PUID} paperless-gpt
fi

# Create necessary directories
mkdir -p /app/prompts /app/config /app/db /home/paperless-gpt

# Set ownership for app and home directories to handle all file permissions
chown -R paperless-gpt:paperless-gpt /app /home/paperless-gpt

# Set HOME env var to user's home directory to ensure configs are written there
export HOME=/home/paperless-gpt

# Drop privileges and execute the main application
echo "Starting application as user paperless-gpt (${PUID}:${PGID})"
exec su-exec paperless-gpt /app/paperless-gpt

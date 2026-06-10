#!/bin/sh

set -eu

LOCKFILE_HASH_FILE="/app/node_modules/.package-lock.sha256"
CURRENT_HASH="$(sha256sum /app/package-lock.json | awk '{ print $1 }')"
INSTALLED_HASH=""

if [ -f "$LOCKFILE_HASH_FILE" ]; then
  INSTALLED_HASH="$(cat "$LOCKFILE_HASH_FILE")"
fi

if [ ! -d /app/node_modules/@uiw/react-codemirror ] || [ "$CURRENT_HASH" != "$INSTALLED_HASH" ]; then
  echo "Installing frontend dependencies..."
  npm ci
  printf '%s' "$CURRENT_HASH" > "$LOCKFILE_HASH_FILE"
fi

exec npm run dev -- --host 0.0.0.0

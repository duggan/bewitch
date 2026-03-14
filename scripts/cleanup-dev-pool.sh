#!/bin/sh
# cleanup-dev-pool.sh — delete dev packages older than 30 days from R2
#
# Removes ~dev .deb files from apt/pool/ and dev tarballs from releases/
# that were uploaded more than 30 days ago.
#
# Usage: scripts/cleanup-dev-pool.sh
#
# Requires: wrangler (npm install -g wrangler), jq
# Environment: BEWITCH_R2_BUCKET (default: "bewitch-apt")

set -eu

BUCKET="${BEWITCH_R2_BUCKET:-bewitch-apt}"
MAX_AGE_DAYS=30

if ! command -v wrangler >/dev/null 2>&1; then
    echo "Error: wrangler not found. Install with: npm install -g wrangler" >&2
    exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
    echo "Error: jq not found" >&2
    exit 1
fi

cutoff=$(date -u -d "-${MAX_AGE_DAYS} days" +%Y-%m-%dT%H:%M:%S 2>/dev/null \
    || date -u -v-${MAX_AGE_DAYS}d +%Y-%m-%dT%H:%M:%S)

deleted=0

cleanup_prefix() {
    prefix="$1"
    pattern="$2"

    objects=$(wrangler r2 object list "$BUCKET" --prefix "$prefix" --remote 2>/dev/null || echo '{"objects":[]}')

    echo "$objects" | jq -r --arg cutoff "$cutoff" --arg pat "$pattern" '
        .objects[]
        | select(.key | test($pat))
        | select(.uploaded < $cutoff)
        | .key
    ' | while read -r key; do
        if [ -n "$key" ]; then
            echo "Deleting old dev artifact: $key"
            wrangler r2 object delete "$BUCKET/$key" --remote
            deleted=$((deleted + 1))
        fi
    done
}

echo "Cleaning up dev artifacts older than ${MAX_AGE_DAYS} days..."

# Clean dev .deb files from pool
cleanup_prefix "apt/pool/main/b/bewitch/" "~dev"

# Clean dev tarballs from releases
cleanup_prefix "releases/" "~dev"

echo "Cleanup complete."

#!/bin/sh
# upload-dists.sh — upload APT repo metadata to Cloudflare R2 bucket
#
# Usage: scripts/upload-dists.sh <site-public-dir>
#
# Uploads all files under <dir>/apt/dists/ to the R2 bucket.
# Also uploads the GPG public key if present.
#
# Requires: wrangler (npm install -g wrangler)
# Environment: BEWITCH_R2_BUCKET (default: "bewitch-apt")

set -eu

BUCKET="${BEWITCH_R2_BUCKET:-bewitch-apt}"

if [ $# -eq 0 ]; then
    echo "Usage: $0 <site-public-dir>" >&2
    exit 1
fi

SITE_PUBLIC="$1"

if [ ! -d "$SITE_PUBLIC/apt/dists" ]; then
    echo "Error: $SITE_PUBLIC/apt/dists not found" >&2
    exit 1
fi

# Verify wrangler is available
if ! command -v wrangler >/dev/null 2>&1; then
    echo "Error: wrangler not found. Install with: npm install -g wrangler" >&2
    exit 1
fi

# Upload all dists metadata files
find "$SITE_PUBLIC/apt/dists" -type f | while read -r file; do
    # Strip the site public prefix to get the R2 key
    r2_key="${file#"$SITE_PUBLIC"/}"
    echo "Uploading $(basename "$file") → r2://$BUCKET/$r2_key"
    wrangler r2 object put "$BUCKET/$r2_key" --file "$file" --remote
done

# Upload GPG public key if present
if [ -f "$SITE_PUBLIC/gpg" ]; then
    echo "Uploading gpg → r2://$BUCKET/gpg"
    wrangler r2 object put "$BUCKET/gpg" --file "$SITE_PUBLIC/gpg" --remote
fi

echo ""
echo "Metadata upload complete."

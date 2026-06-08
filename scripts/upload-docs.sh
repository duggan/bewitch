#!/bin/sh
# upload-docs.sh — snapshot the built docs to Cloudflare R2 for versioned serving.
#
# Usage: scripts/upload-docs.sh <docs-dir> <version-tag>
#   e.g. scripts/upload-docs.sh site/dist/docs v0.8.0
#
# Uploads every file under <docs-dir> to the R2 bucket under docs/<version-tag>/,
# preserving the relative path. functions/docs/[[path]].js then serves them at
# /docs/<version-tag>/**. Assets (CSS/JS/images) are NOT included — versioned
# pages share the latest deploy's static assets at the site root.
#
# Requires: wrangler (npm install -g wrangler)
# Environment: BEWITCH_R2_BUCKET (default: "bewitch-apt")

set -eu

BUCKET="${BEWITCH_R2_BUCKET:-bewitch-apt}"

if [ $# -ne 2 ]; then
    echo "Usage: $0 <docs-dir> <version-tag>" >&2
    exit 1
fi

DOCS_DIR="$1"
VERSION_TAG="$2"

if [ ! -d "$DOCS_DIR" ]; then
    echo "Error: $DOCS_DIR not found" >&2
    exit 1
fi

case "$VERSION_TAG" in
    v*) ;;
    *) echo "Error: version tag must start with 'v' (got '$VERSION_TAG')" >&2; exit 1 ;;
esac

if ! command -v wrangler >/dev/null 2>&1; then
    echo "Error: wrangler not found. Install with: npm install -g wrangler" >&2
    exit 1
fi

find "$DOCS_DIR" -type f | while read -r file; do
    rel="${file#"$DOCS_DIR"/}"
    r2_key="docs/$VERSION_TAG/$rel"
    echo "Uploading $rel → r2://$BUCKET/$r2_key"
    wrangler r2 object put "$BUCKET/$r2_key" --file "$file" --remote
done

echo ""
echo "Docs snapshot for $VERSION_TAG uploaded."

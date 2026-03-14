#!/bin/sh
# upload-versioned-docs.sh — upload versioned docs HTML to R2
#
# Usage: scripts/upload-versioned-docs.sh <dist-dir> <version>
#
# Uploads all HTML files from <dist-dir>/docs/v<version>/ to R2
# at the same path structure (docs/v<version>/*.html).
#
# Requires: wrangler (npm install -g wrangler)
# Environment: BEWITCH_R2_BUCKET (default: "bewitch-apt")

set -eu

BUCKET="${BEWITCH_R2_BUCKET:-bewitch-apt}"

if [ $# -lt 2 ]; then
    echo "Usage: $0 <dist-dir> <version>" >&2
    exit 1
fi

DIST_DIR="$1"
VERSION="$2"
DOCS_DIR="$DIST_DIR/docs/v${VERSION}"

if [ ! -d "$DOCS_DIR" ]; then
    echo "Error: $DOCS_DIR not found" >&2
    exit 1
fi

if ! command -v wrangler >/dev/null 2>&1; then
    echo "Error: wrangler not found. Install with: npm install -g wrangler" >&2
    exit 1
fi

echo "Uploading versioned docs for v${VERSION}..."

find "$DOCS_DIR" -name '*.html' | while read -r file; do
    # Strip dist dir prefix to get R2 key: docs/v0.2.0/installation.html
    r2_key="${file#"$DIST_DIR"/}"
    echo "  $(basename "$file") → r2://$BUCKET/$r2_key"
    wrangler r2 object put "$BUCKET/$r2_key" --file "$file" --remote
done

# Also upload the docs index (docs/v0.2.0.html → docs/v0.2.0.html)
INDEX_FILE="$DIST_DIR/docs/v${VERSION}.html"
if [ -f "$INDEX_FILE" ]; then
    r2_key="docs/v${VERSION}.html"
    echo "  v${VERSION}.html → r2://$BUCKET/$r2_key"
    wrangler r2 object put "$BUCKET/$r2_key" --file "$INDEX_FILE" --remote
fi

echo ""
echo "Versioned docs upload complete (v${VERSION})."

#!/bin/sh
# upload-dev-docs.sh — upload dev docs HTML to R2
#
# Usage: scripts/upload-dev-docs.sh <dist-dir>
#
# Uploads all HTML files from <dist-dir>/docs/dev/ to R2
# at the same path structure (docs/dev/*.html).
#
# Requires: wrangler (npm install -g wrangler)
# Environment: BEWITCH_R2_BUCKET (default: "bewitch-apt")

set -eu

BUCKET="${BEWITCH_R2_BUCKET:-bewitch-apt}"

if [ $# -lt 1 ]; then
    echo "Usage: $0 <dist-dir>" >&2
    exit 1
fi

DIST_DIR="$1"
DOCS_DIR="$DIST_DIR/docs/dev"

if [ ! -d "$DOCS_DIR" ]; then
    echo "Error: $DOCS_DIR not found" >&2
    exit 1
fi

if ! command -v wrangler >/dev/null 2>&1; then
    echo "Error: wrangler not found. Install with: npm install -g wrangler" >&2
    exit 1
fi

echo "Uploading dev docs..."

find "$DOCS_DIR" -name '*.html' | while read -r file; do
    r2_key="${file#"$DIST_DIR"/}"
    echo "  $(basename "$file") → r2://$BUCKET/$r2_key"
    wrangler r2 object put "$BUCKET/$r2_key" --file "$file" --remote
done

# Also upload the docs index (docs/dev.html)
INDEX_FILE="$DIST_DIR/docs/dev.html"
if [ -f "$INDEX_FILE" ]; then
    r2_key="docs/dev.html"
    echo "  dev.html → r2://$BUCKET/$r2_key"
    wrangler r2 object put "$BUCKET/$r2_key" --file "$INDEX_FILE" --remote
fi

echo ""
echo "Dev docs upload complete."

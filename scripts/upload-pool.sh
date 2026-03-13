#!/bin/sh
# upload-pool.sh — upload release artifacts to Cloudflare R2 bucket
#
# Usage: scripts/upload-pool.sh <file> [<file> ...]
#
# Supports .deb and .tar.gz files.
# .deb files are uploaded to apt/pool/main/b/bewitch/
# .tar.gz files are uploaded to releases/
#
# Requires: wrangler (npm install -g wrangler)
# Environment: BEWITCH_R2_BUCKET (default: "bewitch-apt")

set -eu

BUCKET="${BEWITCH_R2_BUCKET:-bewitch-apt}"

if [ $# -eq 0 ]; then
    echo "Usage: $0 <file> [<file> ...]" >&2
    exit 1
fi

# Verify wrangler is available
if ! command -v wrangler >/dev/null 2>&1; then
    echo "Error: wrangler not found. Install with: npm install -g wrangler" >&2
    exit 1
fi

for file in "$@"; do
    if [ ! -f "$file" ]; then
        echo "Error: $file not found" >&2
        exit 1
    fi

    name="$(basename "$file")"

    case "$name" in
        *.deb)
            pkg_name="$(dpkg-deb --field "$file" Package 2>/dev/null || echo "bewitch")"
            first_letter="$(echo "$pkg_name" | cut -c1)"
            r2_key="apt/pool/main/${first_letter}/${pkg_name}/${name}"
            ;;
        *.tar.gz)
            r2_key="releases/${name}"
            ;;
        *)
            echo "Warning: skipping unknown file type: $name" >&2
            continue
            ;;
    esac

    echo "Uploading $name → r2://$BUCKET/$r2_key"
    wrangler r2 object put "$BUCKET/$r2_key" --file "$file" --remote
done

echo ""
echo "Upload complete."

#!/bin/sh
# build-apt-repo.sh — generate APT repository metadata from .deb files
#
# Usage: build-apt-repo.sh <deb-file> [<deb-file> ...]
#
# Requires: dpkg-deb, gpg, sha256sum (or shasum), gzip
# Environment:
#   BEWITCH_GPG_KEY  — GPG key ID (default: "bewitch")
#   SITE_PUBLIC      — output directory (default: site/public relative to script)
#
# Outputs:
#   $SITE_PUBLIC/apt/dists/stable/...   (repo metadata)
#   $SITE_PUBLIC/gpg                    (ASCII-armored public key)

set -eu

GPG_KEY="${BEWITCH_GPG_KEY:-bewitch}"

# Resolve output directory: env var, or relative to script location
if [ -n "${SITE_PUBLIC:-}" ]; then
    SITE_PUBLIC="$SITE_PUBLIC"
elif [ -d "$(dirname "$0")/../site/public" ]; then
    SITE_PUBLIC="$(cd "$(dirname "$0")/../site/public" && pwd)"
else
    SITE_PUBLIC="site/public"
fi

DIST="${APT_DIST:-stable}"
APT_DIR="$SITE_PUBLIC/apt"
DISTS_DIR="$APT_DIR/dists/$DIST"

if [ $# -eq 0 ]; then
    echo "Usage: $0 <deb-file> [<deb-file> ...]" >&2
    exit 1
fi

# Verify all .deb files exist
for deb in "$@"; do
    if [ ! -f "$deb" ]; then
        echo "Error: $deb not found" >&2
        exit 1
    fi
done

# Verify gpg key exists
if ! gpg --list-keys "$GPG_KEY" >/dev/null 2>&1; then
    echo "Error: GPG key '$GPG_KEY' not found" >&2
    echo "Generate one with: gpg --batch --gen-key (see docs)" >&2
    exit 1
fi

# Portable sha256 and md5
sha256() { sha256sum "$1" 2>/dev/null | cut -d' ' -f1 || shasum -a 256 "$1" | cut -d' ' -f1; }
md5hash() { md5sum "$1" 2>/dev/null | cut -d' ' -f1 || md5 -q "$1"; }

# Clean and create directory structure
rm -rf "$APT_DIR"
mkdir -p "$DISTS_DIR/main/binary-amd64"
mkdir -p "$DISTS_DIR/main/binary-arm64"

echo "Building APT repository metadata..."

# Generate Packages file for each architecture
for arch in amd64 arm64; do
    packages_file="$DISTS_DIR/main/binary-$arch/Packages"
    : > "$packages_file"

    for deb in "$@"; do
        deb_name="$(basename "$deb")"

        # Check if this .deb is for this architecture
        deb_arch="$(dpkg-deb --field "$deb" Architecture)"
        if [ "$deb_arch" != "$arch" ] && [ "$deb_arch" != "all" ]; then
            continue
        fi

        # Extract control fields
        control="$(dpkg-deb --field "$deb")"

        # Compute size and checksum
        size="$(wc -c < "$deb" | tr -d ' ')"
        checksum="$(sha256 "$deb")"

        # Determine pool path
        pkg_name="$(dpkg-deb --field "$deb" Package)"
        first_letter="$(echo "$pkg_name" | cut -c1)"
        pool_path="pool/main/${first_letter}/${pkg_name}/${deb_name}"

        # Write package stanza
        echo "$control" >> "$packages_file"
        echo "Filename: $pool_path" >> "$packages_file"
        echo "Size: $size" >> "$packages_file"
        echo "SHA256: $checksum" >> "$packages_file"
        echo "" >> "$packages_file"

        echo "  Added $deb_name ($arch)"
    done

    # Compress
    gzip -k "$packages_file"
done

# Generate Release file
echo "Generating Release file..."

release_file="$DISTS_DIR/Release"

# Collect checksums of all metadata files
cd "$DISTS_DIR"

meta_files=""
for f in main/binary-amd64/Packages main/binary-amd64/Packages.gz \
         main/binary-arm64/Packages main/binary-arm64/Packages.gz; do
    if [ -f "$f" ]; then
        meta_files="$meta_files $f"
    fi
done

# Write Release header
cat > "$release_file" <<RELEASE
Origin: bewitch
Label: bewitch
Suite: $DIST
Codename: $DIST
Architectures: amd64 arm64
Components: main
Date: $(date -u '+%a, %d %b %Y %H:%M:%S UTC')
RELEASE

# SHA256 checksums
echo "SHA256:" >> "$release_file"
for f in $meta_files; do
    hash="$(sha256 "$f")"
    size="$(wc -c < "$f" | tr -d ' ')"
    printf " %s %8s %s\n" "$hash" "$size" "$f" >> "$release_file"
done

# MD5Sum (some older apt versions want this)
echo "MD5Sum:" >> "$release_file"
for f in $meta_files; do
    hash="$(md5hash "$f")"
    size="$(wc -c < "$f" | tr -d ' ')"
    printf " %s %8s %s\n" "$hash" "$size" "$f" >> "$release_file"
done

cd - >/dev/null

# Sign Release file
echo "Signing repository..."
gpg --default-key "$GPG_KEY" --batch --yes --clearsign \
    -o "$DISTS_DIR/InRelease" "$DISTS_DIR/Release"
gpg --default-key "$GPG_KEY" --batch --yes --armor --detach-sign \
    -o "$DISTS_DIR/Release.gpg" "$DISTS_DIR/Release"

# Export public key
gpg --armor --export "$GPG_KEY" > "$SITE_PUBLIC/gpg"

echo ""
echo "APT repository metadata written to $APT_DIR/"
echo "GPG public key written to $SITE_PUBLIC/gpg"

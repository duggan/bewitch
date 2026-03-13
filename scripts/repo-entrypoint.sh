#!/bin/sh
# Import GPG key from mounted file, then run build-apt-repo.sh
set -eu

if [ -z "${GPG_KEY_FILE:-}" ]; then
    echo "Error: GPG_KEY_FILE env var must point to an exported GPG private key" >&2
    exit 1
fi

if [ ! -f "$GPG_KEY_FILE" ]; then
    echo "Error: GPG key file not found: $GPG_KEY_FILE" >&2
    exit 1
fi

gpg --batch --import "$GPG_KEY_FILE" 2>/dev/null

exec build-apt-repo.sh "$@"

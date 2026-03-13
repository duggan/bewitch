#!/bin/sh
# bewitch installer
# Usage: curl -fsSL https://bewitch.dev/install.sh | sudo sh
#
# On Debian/Ubuntu: adds the APT repository and installs the package.
# On other Linux: downloads a pre-built binary tarball and installs it.

set -eu

VERSION="0.1.0"
BASE_URL="https://bewitch.dev"
REPO_URL="${BASE_URL}/apt"
GPG_URL="${BASE_URL}/gpg"
KEYRING="/usr/share/keyrings/bewitch.gpg"
SOURCES_LIST="/etc/apt/sources.list.d/bewitch.list"

# --- helpers ---

info() {
    printf '  \033[1;35m%s\033[0m %s\n' "$1" "$2"
}

error() {
    printf '  \033[1;31merror:\033[0m %s\n' "$1" >&2
    exit 1
}

# --- preflight checks ---

if [ "$(id -u)" -ne 0 ]; then
    error "this installer must be run as root (try: curl -fsSL https://bewitch.dev/install.sh | sudo sh)"
fi

if ! command -v curl >/dev/null 2>&1; then
    error "curl is required but not installed"
fi

# Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    *)
        error "unsupported architecture: $ARCH (bewitch supports x86_64/amd64 and aarch64/arm64)"
        ;;
esac

# Detect OS family
is_apt() {
    command -v apt-get >/dev/null 2>&1
}

echo ""
info "bewitch" "a charming system monitor for Linux"
echo ""

if is_apt; then
    # --- Debian/Ubuntu: use APT repository ---
    info "detected" "Debian/Ubuntu — installing via APT"
    echo ""

    # Download and install GPG key
    info "key" "downloading signing key..."
    curl -fsSL "$GPG_URL" | gpg --dearmor -o "$KEYRING"
    chmod 644 "$KEYRING"

    # Add repository
    info "repo" "adding APT repository..."
    echo "deb [signed-by=$KEYRING arch=$ARCH] $REPO_URL stable main" > "$SOURCES_LIST"

    # Update package list (scoped to bewitch repo only)
    info "update" "fetching package list..."
    apt-get update -o Dir::Etc::sourcelist="$SOURCES_LIST" \
                   -o Dir::Etc::sourceparts="-" \
                   -o APT::Get::List-Cleanup="0" \
                   -qq

    # Install
    info "install" "installing bewitch..."
    apt-get install -y -qq bewitch
else
    # --- Generic Linux: download tarball ---
    info "detected" "non-Debian Linux — installing from binary tarball"
    echo ""

    TARBALL="bewitch-${VERSION}-linux-${ARCH}.tar.gz"
    TARBALL_URL="${BASE_URL}/releases/${TARBALL}"

    info "download" "${TARBALL}..."
    TMP_DIR="$(mktemp -d)"
    trap 'rm -rf "$TMP_DIR"' EXIT

    curl -fsSL "$TARBALL_URL" -o "${TMP_DIR}/${TARBALL}"

    info "extract" "installing to /usr/local/bin/..."
    tar -xzf "${TMP_DIR}/${TARBALL}" -C "$TMP_DIR"

    install -m 755 "${TMP_DIR}/bewitch-${VERSION}-linux-${ARCH}/bewitchd" /usr/local/bin/bewitchd
    install -m 755 "${TMP_DIR}/bewitch-${VERSION}-linux-${ARCH}/bewitch" /usr/local/bin/bewitch

    # Install example config if none exists
    if [ ! -f /etc/bewitch.toml ]; then
        install -m 644 "${TMP_DIR}/bewitch-${VERSION}-linux-${ARCH}/bewitch.example.toml" /etc/bewitch.toml
        info "config" "installed example config to /etc/bewitch.toml"
    fi

    # Create system user if it doesn't exist
    if ! id bewitch >/dev/null 2>&1; then
        useradd -r -s /usr/sbin/nologin bewitch 2>/dev/null || \
            useradd -r -s /sbin/nologin bewitch 2>/dev/null || \
            adduser -S -D -H -s /sbin/nologin bewitch 2>/dev/null || true
        info "user" "created bewitch system user"
    fi

    # Create data directory
    mkdir -p /var/lib/bewitch
    chown bewitch:bewitch /var/lib/bewitch 2>/dev/null || true

    # Install systemd service if systemd is available
    if command -v systemctl >/dev/null 2>&1; then
        install -m 644 "${TMP_DIR}/bewitch-${VERSION}-linux-${ARCH}/bewitchd.service" \
            /etc/systemd/system/bewitchd.service
        systemctl daemon-reload
        info "service" "installed bewitchd.service"
    fi
fi

echo ""
info "done!" "bewitch installed successfully"
echo ""
echo "  Get started:"
echo "    sudo systemctl enable --now bewitchd"
echo "    bewitch"
echo ""
echo "  Configuration: /etc/bewitch.toml"
echo "  Documentation: https://bewitch.dev/docs"
echo ""

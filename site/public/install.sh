#!/bin/sh
# bewitch installer
# Usage: curl -fsSL https://bewitch.dev/install.sh | sudo sh
#
# For dev builds: curl -fsSL https://bewitch.dev/install.sh | BEWITCH_CHANNEL=dev sudo -E sh
#
# Dev channel:
#   curl -fsSL https://bewitch.dev/install-dev.sh | sudo sh
#
# Noninteractive (installs all detected optional deps without prompting):
#   curl -fsSL https://bewitch.dev/install.sh | sudo BEWITCH_NONINTERACTIVE=1 sh
#
# On Debian/Ubuntu: adds the APT repository and installs the package.
# On other Linux: downloads a pre-built binary tarball and installs it.
#
# After installation, detects hardware and offers to install optional
# monitoring tools (intel-gpu-tools, smartmontools, etc.).

set -eu

VERSION="0.3.1"
CHANNEL="${BEWITCH_CHANNEL:-stable}"
NONINTERACTIVE="${BEWITCH_NONINTERACTIVE:-${NONINTERACTIVE:-0}}"
case "${CI:-}" in true|1) NONINTERACTIVE=1 ;; esac
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

ask() {
    if [ "$NONINTERACTIVE" = "1" ]; then return 0; fi
    printf '  \033[1;35m?\033[0m %s [y/N] ' "$1"
    if [ -e /dev/tty ]; then
        read -r answer < /dev/tty
    else
        printf 'n\n'
        return 1
    fi
    case "$answer" in [Yy]*) return 0 ;; *) return 1 ;; esac
}

has_intel_gpu() {
    for d in /sys/class/drm/card*/device/driver; do
        [ -e "$d" ] || continue
        case "$(basename "$(readlink -f "$d")")" in i915|xe) return 0 ;; esac
    done
    return 1
}

has_nvidia_gpu() {
    for f in /sys/bus/pci/devices/*/vendor; do
        [ -e "$f" ] || continue
        [ "$(cat "$f")" = "0x10de" ] || continue
        class="$(cat "$(dirname "$f")/class" 2>/dev/null)" || continue
        case "$class" in 0x0300*|0x0302*) return 0 ;; esac
    done
    return 1
}

pkg_install() {
    if command -v apt-get >/dev/null 2>&1; then
        apt-get install -y -qq "$@" 2>/dev/null
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y -q "$@" 2>/dev/null
    elif command -v yum >/dev/null 2>&1; then
        yum install -y -q "$@" 2>/dev/null
    elif command -v pacman >/dev/null 2>&1; then
        pacman -S --noconfirm "$@" 2>/dev/null
    elif command -v zypper >/dev/null 2>&1; then
        zypper install -y "$@" 2>/dev/null
    elif command -v apk >/dev/null 2>&1; then
        apk add --quiet "$@" 2>/dev/null
    else
        return 1
    fi
}

# Offer to install a package. Args: tool_binary package_name description
offer_pkg() {
    _tool="$1"; _pkg="$2"; _desc="$3"
    if command -v "$_tool" >/dev/null 2>&1; then
        info "ok" "$_pkg already installed ($_desc)"
        return 0
    fi
    if ask "Install $_pkg for $_desc?"; then
        info "install" "installing $_pkg..."
        if pkg_install "$_pkg"; then
            info "ok" "$_pkg installed"
        else
            info "skip" "could not install $_pkg — install it manually via your package manager"
        fi
    fi
}

install_extras() {
    [ "$(uname -s)" = "Linux" ] || return 0

    _found=""

    # smartmontools — on Debian/Ubuntu, already handled via Recommends in the .deb
    if ! is_apt && ! command -v smartctl >/dev/null 2>&1; then _found=1; fi
    if has_intel_gpu && ! command -v intel_gpu_top >/dev/null 2>&1; then _found=1; fi
    if has_nvidia_gpu && ! command -v nvidia-smi >/dev/null 2>&1; then _found=1; fi

    # Nothing to offer
    [ -n "$_found" ] || return 0

    echo ""
    info "extras" "checking for optional monitoring tools..."

    if ! is_apt; then
        offer_pkg smartctl smartmontools "disk SMART health"
    fi

    if has_intel_gpu; then
        offer_pkg intel_gpu_top intel-gpu-tools "Intel GPU monitoring"
    fi

    if has_nvidia_gpu && ! command -v nvidia-smi >/dev/null 2>&1; then
        info "note" "NVIDIA GPU detected — install your distribution's NVIDIA driver package for GPU monitoring"
    fi
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

# For dev channel on non-APT systems, resolve the latest dev version for tarball downloads.
# The dev build workflow writes the current version to releases/dev-version.txt in R2.
if [ "$CHANNEL" != "stable" ] && ! is_apt; then
    info "version" "resolving latest dev version..."
    DEV_VERSION="$(curl -fsSL "${BASE_URL}/releases/dev-version.txt" 2>/dev/null)" || true
    if [ -n "$DEV_VERSION" ]; then
        VERSION="$DEV_VERSION"
    else
        error "could not resolve dev version — use APT on Debian/Ubuntu or download from https://github.com/duggan/bewitch/releases"
    fi
fi

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
    echo "deb [signed-by=$KEYRING arch=$ARCH] $REPO_URL $CHANNEL main" > "$SOURCES_LIST"

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

install_extras

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

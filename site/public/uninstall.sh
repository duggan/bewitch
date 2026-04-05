#!/bin/sh
# bewitch uninstaller
# Usage: curl -fsSL https://bewitch.dev/uninstall.sh | sudo sh
#
# Removes bewitch binaries, systemd service, config, data, and system user.
# On Debian/Ubuntu (APT installs): removes the package and APT repository.
# On other Linux (tarball installs): removes files directly.
#
# Pass KEEP_DATA=1 to preserve /var/lib/bewitch (database and archives).

set -eu

KEEP_DATA="${KEEP_DATA:-0}"
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
    printf '  \033[1;35m?\033[0m %s [y/N] ' "$1"
    if [ -e /dev/tty ]; then
        read -r answer < /dev/tty
    else
        printf 'n\n'
        return 1
    fi
    case "$answer" in [Yy]*) return 0 ;; *) return 1 ;; esac
}

# --- preflight checks ---

if [ "$(id -u)" -ne 0 ]; then
    error "this uninstaller must be run as root (try: curl -fsSL https://bewitch.dev/uninstall.sh | sudo sh)"
fi

echo ""
info "bewitch" "uninstaller"
echo ""

is_apt() {
    command -v apt-get >/dev/null 2>&1
}

is_deb_installed() {
    dpkg -s bewitch >/dev/null 2>&1
}

# --- stop the service ---

if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files bewitchd.service >/dev/null 2>&1; then
    info "service" "stopping bewitchd..."
    systemctl stop bewitchd 2>/dev/null || true
    systemctl disable bewitchd 2>/dev/null || true
fi

# --- remove package or files ---

if is_apt && is_deb_installed; then
    # APT path: remove the package
    info "remove" "removing bewitch package..."
    if [ "$KEEP_DATA" = "1" ]; then
        apt-get remove -y -qq bewitch
    else
        apt-get purge -y -qq bewitch
    fi

    # Clean up APT repository
    if [ -f "$SOURCES_LIST" ] || [ -f "$KEYRING" ]; then
        info "repo" "removing APT repository..."
        rm -f "$SOURCES_LIST"
        rm -f "$KEYRING"
        apt-get update -o Dir::Etc::sourcelist="$SOURCES_LIST" \
                       -o Dir::Etc::sourceparts="-" \
                       -o APT::Get::List-Cleanup="0" \
                       -qq 2>/dev/null || true
    fi
else
    # Tarball path: remove files directly
    info "remove" "removing binaries..."
    rm -f /usr/local/bin/bewitchd
    rm -f /usr/local/bin/bewitch
    # Also check /usr/bin in case of manual install
    rm -f /usr/bin/bewitchd
    rm -f /usr/bin/bewitch

    # Remove systemd service
    if [ -f /etc/systemd/system/bewitchd.service ]; then
        info "service" "removing bewitchd.service..."
        rm -f /etc/systemd/system/bewitchd.service
        systemctl daemon-reload 2>/dev/null || true
    fi

    # Remove config
    if [ -f /etc/bewitch.toml ]; then
        info "config" "removing /etc/bewitch.toml..."
        rm -f /etc/bewitch.toml
    fi

    # Remove data directory
    if [ "$KEEP_DATA" = "1" ]; then
        info "keep" "/var/lib/bewitch preserved (KEEP_DATA=1)"
    elif [ -d /var/lib/bewitch ]; then
        info "data" "removing /var/lib/bewitch..."
        rm -rf /var/lib/bewitch
    fi

    # Remove system user
    if getent passwd bewitch >/dev/null 2>&1; then
        info "user" "removing bewitch system user..."
        userdel bewitch 2>/dev/null || deluser --system bewitch 2>/dev/null || true
    fi
    if getent group bewitch >/dev/null 2>&1; then
        groupdel bewitch 2>/dev/null || delgroup --system bewitch 2>/dev/null || true
    fi
fi

echo ""
info "done!" "bewitch has been uninstalled"
echo ""

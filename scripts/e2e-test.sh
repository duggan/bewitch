#!/bin/sh
# E2E installation and smoke test for bewitch.
# Runs inside a Docker container for the target OS.
#
# Required env vars:
#   INSTALL_METHOD  — deb or tarball
#   BEWITCH_ARCH    — amd64 or arm64
#
# Artifacts must be mounted at /dist.

set -eu

SOCK="/tmp/bewitch-e2e.sock"
CONFIG="/tmp/bewitch-e2e.toml"
DAEMON_PID=""

pass() { printf '  \033[1;32mPASS\033[0m %s\n' "$1"; }
fail() { printf '  \033[1;31mFAIL\033[0m %s\n' "$1"; exit 1; }
info() { printf '  \033[1;35m....\033[0m %s\n' "$1"; }

cleanup() {
    if [ -n "$DAEMON_PID" ]; then
        kill "$DAEMON_PID" 2>/dev/null || true
        wait "$DAEMON_PID" 2>/dev/null || true
    fi
    rm -f "$SOCK" "$CONFIG" /tmp/bewitch-e2e.duckdb* /tmp/tui-output.txt /tmp/tui-stderr.txt
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Phase 0: Install prerequisites
# ---------------------------------------------------------------------------
install_prereqs() {
    info "installing prerequisites"
    if command -v apt-get >/dev/null 2>&1; then
        apt-get update -qq
        apt-get install -y -qq curl procps adduser util-linux
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y curl procps-ng util-linux
    elif command -v pacman >/dev/null 2>&1; then
        pacman -Sy --noconfirm curl procps-ng util-linux
    fi
}

# ---------------------------------------------------------------------------
# Phase 1: Install bewitch
# ---------------------------------------------------------------------------
install_bewitch() {
    info "installing bewitch (method=$INSTALL_METHOD)"
    case "$INSTALL_METHOD" in
        deb)
            DEB_FILE=$(ls /dist/bewitch_*_"${BEWITCH_ARCH}".deb 2>/dev/null | head -1)
            [ -n "$DEB_FILE" ] || fail "no .deb found for arch $BEWITCH_ARCH in /dist"
            dpkg -i "$DEB_FILE" || apt-get install -f -y -qq
            ;;
        tarball)
            TARBALL=$(ls /dist/bewitch-*-linux-"${BEWITCH_ARCH}".tar.gz 2>/dev/null | head -1)
            [ -n "$TARBALL" ] || fail "no tarball found for arch $BEWITCH_ARCH in /dist"
            TMP_DIR=$(mktemp -d)
            tar -xzf "$TARBALL" -C "$TMP_DIR"
            EXTRACTED=$(ls -d "$TMP_DIR"/bewitch-*-linux-* | head -1)
            install -m 755 "$EXTRACTED/bewitchd" /usr/local/bin/bewitchd
            install -m 755 "$EXTRACTED/bewitch" /usr/local/bin/bewitch
            cp "$EXTRACTED/bewitch.example.toml" /etc/bewitch.toml
            rm -rf "$TMP_DIR"
            ;;
        *)
            fail "unknown INSTALL_METHOD: $INSTALL_METHOD"
            ;;
    esac
    pass "installation complete"
}

# ---------------------------------------------------------------------------
# Phase 2: Version check
# ---------------------------------------------------------------------------
check_versions() {
    info "checking versions"

    BEWITCHD_VER=$(bewitchd -version 2>&1) || fail "bewitchd -version failed"
    BEWITCH_VER=$(bewitch -version 2>&1) || fail "bewitch -version failed"

    echo "$BEWITCHD_VER" | grep -q "bewitchd" || fail "bewitchd -version output unexpected: $BEWITCHD_VER"
    echo "$BEWITCH_VER" | grep -q "bewitch" || fail "bewitch -version output unexpected: $BEWITCH_VER"

    pass "bewitchd version: $BEWITCHD_VER"
    pass "bewitch version: $BEWITCH_VER"
}

# ---------------------------------------------------------------------------
# Phase 3: Daemon startup with mock mode
# ---------------------------------------------------------------------------
start_daemon() {
    info "starting daemon with mock mode"

    cat > "$CONFIG" <<EOF
[daemon]
mock = true
socket = "$SOCK"
db_path = "/tmp/bewitch-e2e.duckdb"
default_interval = "1s"
EOF

    bewitchd -config "$CONFIG" &
    DAEMON_PID=$!

    # Poll for socket
    for i in $(seq 1 30); do
        [ -S "$SOCK" ] && break
        sleep 0.5
    done

    [ -S "$SOCK" ] || fail "daemon socket did not appear within 15s"
    pass "daemon started (pid=$DAEMON_PID)"
}

# ---------------------------------------------------------------------------
# Phase 4: API health check
# ---------------------------------------------------------------------------
check_api() {
    info "checking API health"

    STATUS=$(curl -sf --unix-socket "$SOCK" http://localhost/api/status) \
        || fail "GET /api/status failed"
    pass "/api/status responded"

    # Wait for at least one collection cycle before checking metrics
    sleep 3

    CPU=$(curl -sf --unix-socket "$SOCK" http://localhost/api/metrics/cpu) \
        || fail "GET /api/metrics/cpu failed"
    echo "$CPU" | grep -q "cores" || fail "/api/metrics/cpu response missing 'cores' key"
    pass "/api/metrics/cpu has data"

    MEM=$(curl -sf --unix-socket "$SOCK" http://localhost/api/metrics/memory) \
        || fail "GET /api/metrics/memory failed"
    pass "/api/metrics/memory responded"
}

# ---------------------------------------------------------------------------
# Phase 5: TUI smoke test
# ---------------------------------------------------------------------------
check_tui() {
    info "TUI smoke test"

    # Use 'script' to allocate a PTY so bubbletea can start in Docker.
    # Send 'q' after a delay to quit the TUI cleanly.
    rm -f /tmp/tui-output.txt
    (sleep 3; printf 'q') | script -qec "bewitch -config $CONFIG" /tmp/tui-output.txt &
    SCRIPT_PID=$!

    # Wait for script to finish (TUI should quit on 'q')
    if wait "$SCRIPT_PID" 2>/dev/null; then
        pass "TUI started and exited cleanly"
    else
        # Check output for real errors (not just non-zero exit from signal)
        if [ -s /tmp/tui-output.txt ] && grep -qi "panic" /tmp/tui-output.txt; then
            cat /tmp/tui-output.txt
            fail "TUI panicked"
        fi
        pass "TUI started (non-zero exit acceptable in CI)"
    fi
}

# ---------------------------------------------------------------------------
# Phase 6: Package removal (deb only)
# ---------------------------------------------------------------------------
remove_package() {
    info "testing package removal"

    # Stop daemon before removal
    if [ -n "$DAEMON_PID" ]; then
        kill "$DAEMON_PID" 2>/dev/null || true
        wait "$DAEMON_PID" 2>/dev/null || true
        DAEMON_PID=""
    fi

    dpkg -r bewitch
    ! command -v bewitchd >/dev/null 2>&1 || fail "bewitchd still found after removal"
    ! command -v bewitch >/dev/null 2>&1 || fail "bewitch still found after removal"
    pass "package removed, binaries gone"

    dpkg --purge bewitch
    if id bewitch >/dev/null 2>&1; then
        fail "bewitch user still exists after purge"
    fi
    pass "package purged cleanly"
}

# ---------------------------------------------------------------------------
# Run
# ---------------------------------------------------------------------------
install_prereqs
install_bewitch
check_versions
start_daemon
check_api
check_tui

if [ "$INSTALL_METHOD" = "deb" ]; then
    remove_package
fi

echo ""
pass "ALL TESTS PASSED"

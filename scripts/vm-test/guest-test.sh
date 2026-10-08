#!/usr/bin/env bash
# Runs inside the test VM as root (see run-vm.sh). Installs the previous stable
# release from the APT repo, upgrades to the freshly built package, and checks
# the behaviours that only show up under real systemd on real (emulated)
# hardware. Every check reports PASS/FAIL and the script carries on, so one run
# shows everything that's broken; it exits non-zero if anything failed.
#
# Usage: guest-test.sh <path/to/bewitch.deb>
set -uo pipefail

DEB=$1
SOCK=/run/bewitch/bewitch.sock
FAILED=0
export DEBIAN_FRONTEND=noninteractive

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*"; FAILED=1; }
check() { local desc=$1; shift; if "$@"; then pass "$desc"; else fail "$desc"; fi; }
api() { curl -fsS --max-time 10 --unix-socket "$SOCK" "http://x$1"; }
query() { curl -fsS --max-time 30 --unix-socket "$SOCK" -X POST -H 'Content-Type: application/json' -d "{\"sql\":\"$1\"}" http://x/api/query; }
wait_for() { # wait_for <seconds> <cmd...>
  local t=$1; shift
  for _ in $(seq 1 "$t"); do "$@" >/dev/null 2>&1 && return 0; sleep 1; done
  return 1
}

echo "::group::Guest setup"
apt-get update -qq
apt-get install -y -qq lvm2 smartmontools curl jq gnupg >/dev/null
# LVM on the emulated NVMe drive: exercises the /dev/mapper → dm-N I/O lookup
# and NVMe SMART through the LV's physical device.
pvcreate -q /dev/nvme0n1
vgcreate -q testvg /dev/nvme0n1
lvcreate -q -y -n data -L 2G testvg
mkfs.ext4 -q /dev/testvg/data
mkdir -p /srv/data
mount /dev/testvg/data /srv/data
lsblk
echo "::endgroup::"

echo "::group::Install previous stable from APT"
curl -fsSL https://bewitch.dev/gpg | gpg --dearmor -o /usr/share/keyrings/bewitch.gpg
echo "deb [signed-by=/usr/share/keyrings/bewitch.gpg] https://bewitch.dev/apt stable main" > /etc/apt/sources.list.d/bewitch.list
apt-get update -qq
apt-get install -y -qq bewitch >/dev/null
dpkg-query -W bewitch
check "previous stable bewitchd is running" wait_for 30 systemctl is-active --quiet bewitchd
echo "::endgroup::"

echo "::group::Upgrade to the built package"
UPGRADE_START=$(date '+%Y-%m-%d %H:%M:%S')
if [ "${BASELINE:-0}" = 1 ]; then
  # Negative control: run every check against the previous stable release only.
  # Checks covering bugs fixed since then must FAIL here, proving they detect them.
  echo "BASELINE=1: skipping the upgrade; expect failures"
else
  apt-get install -y "$DEB" 2>&1 | tail -5
fi
dpkg-query -W bewitch
echo "::endgroup::"

echo "::group::Service and sandbox"
check "bewitchd active after upgrade" wait_for 30 systemctl is-active --quiet bewitchd
check "bewitch-smart.timer enabled on upgrade" systemctl is-enabled --quiet bewitch-smart.timer
check "bewitch-smart.timer active" systemctl is-active --quiet bewitch-smart.timer
PID=$(systemctl show -p MainPID --value bewitchd)
check "bewitchd runs as the bewitch user" test "$(ps -o user= -p "$PID" | tr -d ' ')" = bewitch
CAPEFF=$(awk '/^CapEff/{print $2}' "/proc/$PID/status")
check "bewitchd does not hold CAP_SYS_ADMIN (CapEff=$CAPEFF)" test $(( (0x$CAPEFF >> 21) & 1 )) -eq 0
check "API responds" wait_for 30 sh -c "curl -fsS --unix-socket $SOCK http://x/api/status | jq -e '.status == \"ok\"'"
echo "::endgroup::"

echo "::group::NVMe SMART helper"
systemctl start bewitch-smart.service || true
check "helper wrote an NVMe snapshot" wait_for 60 test -s /var/lib/bewitch/smart/nvme0n1.json
# Require real data, not just "available": 0.8.0 reported NVMe SMART as
# available+healthy with every field zero when the health log was unreadable.
check "snapshot has real NVMe SMART data" jq -e '.info.Available == true and .info.Temperature > 0' /var/lib/bewitch/smart/nvme0n1.json
systemctl show bewitch-smart.service -p Result -p ExecMainStatus
# The daemon reads snapshots at its SMART refresh; restart so it picks this one
# up now rather than in smart_interval (5m).
systemctl restart bewitchd
wait_for 30 sh -c "curl -fsS --unix-socket $SOCK http://x/api/metrics/disk | jq -e '.disks[] | select(.mount == \"/srv/data\")'" || true
DISK=$(api /api/metrics/disk)
echo "$DISK" | jq -c '[.disks[] | {mount, device, smart_available}]'
check "LVM mount /srv/data is listed" sh -c "echo '$DISK' | jq -e '.disks[] | select(.mount == \"/srv/data\")' >/dev/null"
check "unprivileged daemon reports real NVMe SMART data (via helper)" sh -c "echo '$DISK' | jq -e '.disks[] | select(.mount == \"/srv/data\") | .smart_available == true and (.smart_temperature // 0) > 0' >/dev/null"
check "no phantom sandbox mounts (/var/lib/bewitch, /var/tmp)" sh -c "! echo '$DISK' | jq -e '.disks[] | select(.mount == \"/var/lib/bewitch\" or .mount == \"/var/tmp\")' >/dev/null"
echo "::endgroup::"

echo "::group::LVM disk I/O"
( for _ in $(seq 1 30); do dd if=/dev/zero of=/srv/data/load bs=1M count=64 oflag=direct status=none; sync; done ) &
LOAD=$!
io_seen() { api /api/metrics/disk | jq -e '.disks[] | select(.mount == "/srv/data") | .write_bytes_sec > 0' >/dev/null; }
check "write I/O on the LVM volume is non-zero" wait_for 45 io_seen
kill "$LOAD" 2>/dev/null; wait "$LOAD" 2>/dev/null
echo "::endgroup::"

echo "::group::Compaction then restart"
V1=$(query "SELECT version FROM schema_version" | jq -r '.rows[0][0]')
check "manual compaction succeeds" curl -fsS --max-time 120 --unix-socket "$SOCK" -X POST http://x/api/compact
systemctl restart bewitchd
check "bewitchd restarts cleanly after compaction" wait_for 30 sh -c "curl -fsS --unix-socket $SOCK http://x/api/status | jq -e '.status == \"ok\"'"
V2=$(query "SELECT version FROM schema_version" | jq -r '.rows[0][0]')
check "schema_version survives compaction ($V1 -> $V2)" test -n "$V1" -a "$V1" = "$V2"
check "no restart loop" test "$(systemctl show -p NRestarts --value bewitchd)" = 0
echo "::endgroup::"

echo "::group::Journal"
JOURNAL=$(journalctl -u bewitchd --since "$UPGRADE_START" --no-pager -o cat)
check "no RAPL 'unreadable' warning on a host without RAPL" sh -c "! echo \"\$0\" | grep -q 'RAPL energy counters unreadable'" "$JOURNAL"
ERRORS=$(echo "$JOURNAL" | grep -E ' (ERRO|FATA) ' || true)
if [ -n "$ERRORS" ]; then echo "$ERRORS"; fi
check "no ERROR/FATAL lines since the upgrade" test -z "$ERRORS"
echo "::endgroup::"

if [ "$FAILED" -ne 0 ]; then
  echo "Some checks failed."
  exit 1
fi
echo "All checks passed."

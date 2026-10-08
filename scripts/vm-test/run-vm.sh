#!/usr/bin/env bash
# Boot a Debian 13 VM under QEMU/KVM and run guest-test.sh in it against a
# freshly built bewitch .deb. Runs on a GitHub-hosted Ubuntu runner (which has
# /dev/kvm). The VM gets an emulated NVMe drive (QEMU implements the SMART /
# health log) so the NVMe SMART helper, LVM disk I/O and the hardened systemd
# units are exercised for real — the containers in e2e.yml have no systemd.
#
# Usage: run-vm.sh <path/to/bewitch_*.deb> [workdir]
set -euo pipefail

DEB=$(realpath "$1")
WORK=${2:-$PWD/vm-work}
HERE=$(cd "$(dirname "$0")" && pwd)
IMG_BASE=https://cloud.debian.org/images/cloud/trixie/latest
IMG_NAME=debian-13-genericcloud-amd64.qcow2
SSH_PORT=2222

mkdir -p "$WORK"
cd "$WORK"

echo "::group::Host setup"
sudo apt-get update -qq
sudo apt-get install -y -qq qemu-system-x86 qemu-utils cloud-image-utils >/dev/null
# GitHub's Ubuntu runners expose /dev/kvm but not to the runner user by default.
echo 'KERNEL=="kvm", GROUP="kvm", MODE="0666", OPTIONS+="static_node=kvm"' | sudo tee /etc/udev/rules.d/99-kvm.rules >/dev/null
sudo udevadm control --reload-rules
sudo udevadm trigger --name-match=kvm
ls -l /dev/kvm
echo "::endgroup::"

echo "::group::Debian cloud image"
curl -fsSLO "$IMG_BASE/$IMG_NAME"
curl -fsSL "$IMG_BASE/SHA512SUMS" -o SHA512SUMS
# Verify before use: refuse to boot an image that doesn't match the published sum.
grep " $IMG_NAME\$" SHA512SUMS | sha512sum -c -
qemu-img create -q -f qcow2 -F qcow2 -b "$IMG_NAME" disk.qcow2 20G
qemu-img create -q -f qcow2 nvme.qcow2 4G
echo "::endgroup::"

echo "::group::cloud-init seed"
ssh-keygen -q -t ed25519 -N "" -f id_vm
cat > user-data <<EOF
#cloud-config
users:
  - name: tester
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    ssh_authorized_keys:
      - $(cat id_vm.pub)
EOF
echo "instance-id: bewitch-vm-test" > meta-data
cloud-localds seed.iso user-data meta-data
echo "::endgroup::"

echo "::group::Boot VM"
qemu-system-x86_64 \
  -enable-kvm -cpu host -m 4096 -smp 2 \
  -drive file=disk.qcow2,if=virtio \
  -drive file=seed.iso,if=virtio,format=raw \
  -drive file=nvme.qcow2,if=none,id=nvm \
  -device nvme,serial=bewitchnvme0,drive=nvm \
  -netdev user,id=n0,hostfwd=tcp:127.0.0.1:${SSH_PORT}-:22 \
  -device virtio-net-pci,netdev=n0 \
  -display none -serial file:serial.log \
  -daemonize -pidfile qemu.pid
echo "::endgroup::"

SSH=(ssh -i id_vm -p "$SSH_PORT" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=5 -o LogLevel=ERROR tester@127.0.0.1)
SCP=(scp -i id_vm -P "$SSH_PORT" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR)

cleanup() {
  # Always try to pull the daemon journal out before tearing the VM down.
  "${SSH[@]}" 'sudo journalctl -u bewitchd -u bewitch-smart --no-pager' > journal.log 2>/dev/null || true
  [ -f qemu.pid ] && kill "$(cat qemu.pid)" 2>/dev/null || true
}
trap cleanup EXIT

echo "Waiting for SSH..."
for i in $(seq 1 60); do
  if "${SSH[@]}" true 2>/dev/null; then break; fi
  if [ "$i" = 60 ]; then echo "VM never came up; serial log:"; tail -50 serial.log; exit 1; fi
  sleep 5
done
"${SSH[@]}" 'cloud-init status --wait >/dev/null 2>&1 || true; uname -a'

"${SCP[@]}" "$DEB" "$HERE/guest-test.sh" tester@127.0.0.1:/tmp/
"${SSH[@]}" "sudo bash /tmp/guest-test.sh /tmp/$(basename "$DEB")"

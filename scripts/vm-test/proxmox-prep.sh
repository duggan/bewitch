#!/usr/bin/env bash
# Turn the Debian 13 test VM into a Proxmox VE 9 host, following Proxmox's
# "Install Proxmox VE on Debian 13 Trixie" procedure. Two stages because the
# Proxmox kernel must be booted before proxmox-ve is installed:
#
#   proxmox-prep.sh stage1   # repo + Proxmox kernel; the caller then reboots
#   proxmox-prep.sh stage2   # proxmox-ve (pmxcfs, /etc/pve), after the reboot
#
# Runs as root inside the VM (see run-vm.sh).
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive

case "${1:-}" in
stage1)
  # Proxmox needs the hostname to resolve to a non-loopback address. Under
  # QEMU user networking the guest is always 10.0.2.15.
  hostnamectl set-hostname pve-test
  sed -i '/pve-test/d' /etc/hosts
  echo "10.0.2.15 pve-test.local pve-test" >> /etc/hosts
  hostname --ip-address

  curl -fsSL https://enterprise.proxmox.com/debian/proxmox-archive-keyring-trixie.gpg \
    -o /usr/share/keyrings/proxmox-archive-keyring.gpg
  cat > /etc/apt/sources.list.d/pve-install-repo.sources <<'EOF'
Types: deb
URIs: http://download.proxmox.com/debian/pve
Suites: trixie
Components: pve-no-subscription
Signed-By: /usr/share/keyrings/proxmox-archive-keyring.gpg
EOF
  apt-get update -qq
  # The cloud image's grub-pc remembers an install device that doesn't exist in
  # this VM, so the upgrade's grub-pc postinst fails non-interactively. Point it
  # at the real boot disk first.
  BOOTDISK=/dev/$(lsblk -no PKNAME "$(findmnt -no SOURCE /)")
  echo "grub-pc grub-pc/install_devices multiselect $BOOTDISK" | debconf-set-selections
  apt-get full-upgrade -y -q 2>&1 | tail -60
  apt-get install -y -q proxmox-default-kernel 2>&1 | tail -60
  ls /boot/vmlinuz-*pve*
  ;;
stage2)
  uname -r | grep -q pve || { echo "not running a Proxmox kernel: $(uname -r)"; exit 1; }
  # proxmox-ve pulls in postfix, which asks how to configure mail.
  echo "postfix postfix/main_mailer_type select Local only" | debconf-set-selections
  echo "postfix postfix/mailname string pve-test.local" | debconf-set-selections
  apt-get install -y -q proxmox-ve postfix open-iscsi chrony 2>&1 | tail -60
  # The package adds the enterprise (subscription-only) repos; without a
  # subscription their 401s make every apt-get update fail.
  rm -f /etc/apt/sources.list.d/pve-enterprise.sources /etc/apt/sources.list.d/ceph.sources \
        /etc/apt/sources.list.d/pve-enterprise.list /etc/apt/sources.list.d/ceph.list
  systemctl is-active pve-cluster
  findmnt /etc/pve
  pveversion
  ;;
*)
  echo "usage: $0 stage1|stage2" >&2
  exit 2
  ;;
esac

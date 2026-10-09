+++
title = "Running on Proxmox VE"
description = "What works out of the box on a Proxmox host, what to set up, and how to pull guest and storage stats from the PVE API."
weight = 25

[extra]
group = "Get started"
+++

Proxmox VE is Debian-based, so bewitch installs on the host from the APT repository like any
Debian system, and the packaged daemon runs unprivileged. This page covers what works out of the box, the few things to set up, and how
to pull guest and storage stats from the Proxmox API.

## Install on the host, not in a container

Install bewitch on the Proxmox host itself (the Debian package or the install script, as on any
Debian system). Inside an LXC container it can't see block devices or SMART, and
lxcfs virtualizes `/proc/meminfo` and `/proc/stat`, so CPU and memory describe the container
rather than the machine.

## What works out of the box

- **Disks on LVM** (`/dev/mapper/pve-root` and friends): space and I/O rates, with SMART read
  from the physical disk underneath.
- **NVMe SMART**: recent kernels require `CAP_SYS_ADMIN` to read the NVMe health log. The daemon
  doesn't hold that capability; the packaged `bewitch-smart.timer` reads NVMe drives every
  5 minutes in a separate sandboxed helper. It's enabled automatically. Check it with
  `systemctl status bewitch-smart.timer`.
- **ZFS** (including a ZFS root): each mounted dataset is listed with its space and I/O, and
  SMART is read from the pool's disks. In a mirror or RAID-Z, the mount shows a failing disk
  first, and every disk's SMART is stored and alertable. The ARC (ZFS's read cache) counts as
  cache memory, not used memory.
- **`/etc/pve`** (pmxcfs, the cluster config filesystem) is excluded from the disk list by default.
- **Power** (Intel/AMD RAPL), temperatures, network, processes: as on any host.

## Things to know

- **Every VM is a `kvm` process.** In the process view and its history, all VMs are grouped
  under one `kvm` name. For per-guest numbers, use the API sources below.
- **Container processes show up as host processes.** A `postgres` running in CT 105 appears as
  an ordinary `postgres`.
- **Guest network interfaces multiply.** Each guest NIC adds `tap…`/`veth…`, `fwbr…`, `fwln…` and
  `fwpr…` interfaces on the host, all carrying the same traffic. Use the Network tab's selection
  (space / `a`) to chart the ones you care about, typically `vmbr0` and your physical NICs.
- **ZFS I/O is per dataset and logical**: it counts reads the ARC answered, not just disk
  reads. Guest disks on zvols aren't mounted on the host, so they aren't listed. Container
  volumes (`subvol-…` datasets) are, one per container; exclude `/rpool/data/` if that's too
  many.

## Guests, storage and quorum from the Proxmox API

The Proxmox API already knows per-guest status, storage usage (including LVM-thin pool usage,
which needs root to read directly) and cluster quorum. Bewitch's
[custom sources](@/docs/custom-sources.md) can poll it with no extra software.
[`examples/sources.d/proxmox.toml`](https://github.com/duggan/bewitch/blob/main/examples/sources.d/proxmox.toml)
is a ready-made starting point; the Debian package installs it as
`/usr/share/bewitch/examples/sources.d/proxmox.toml`:

1. **Create a read-only API token** (the `PVEAuditor` role is enough):

   ```sh
   pveum user add bewitch@pve
   pveum acl modify / --users bewitch@pve --roles PVEAuditor
   pveum user token add bewitch@pve monitor --privsep 0
   ```

2. **Install the example** into your sources directory (it holds the token, so keep it
   `0640 root:bewitch`), then paste the token secret into its `header_value` lines and set your
   node name in the node-status path.

3. **Pin the API's certificate.** Proxmox serves `:8006` with a self-signed certificate. Get its
   SHA-256 fingerprint from the web UI (Node → System → Certificates) or run
   `openssl x509 -in /etc/pve/local/pve-ssl.pem -noout -fingerprint -sha256`, and put it in each
   `[custom_source.tls] fingerprint`. See [Self-signed HTTPS](@/docs/custom-sources.md#self-signed-https).

4. Restart bewitchd. The sources appear under the TUI's **Services** tab:

   - **`pve-node`**: CPU, load, memory, KSM sharing, root filesystem, PVE and kernel versions.
   - **`pve-guests`**: running, stopped and locked guest counts (a lock means a backup, snapshot or
     migration is in flight). Per-guest CPU and memory can be added one block per guest.
   - **`pve-storage`**: LVM-thin pool usage.
   - **`pve-cluster`**: nodes online and quorum.

   bewitch stores and charts these numbers, and includes them on its own Prometheus
   [`/metrics`](@/docs/api.md#prometheus-metrics) endpoint as `bewitch_custom_value`. Alert rules
   can't use custom metrics yet.

Authenticate with `type = "header"` and `header_name = "Authorization"`. Proxmox expects
`PVEAPIToken=…` rather than `Bearer …`, so the `bearer` auth type won't work.

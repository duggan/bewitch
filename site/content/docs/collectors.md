+++
title = "Collectors"
description = "The ten built-in collectors — CPU, memory, load, disk, network, ECC, temperature, power, GPU, process — and how to tune their intervals."
weight = 80

[extra]
group = "Reference"
+++

Bewitch has ten built-in collectors: CPU, memory, load average, disk, network, ECC, temperature,
power, GPU and process. [Custom sources](@/docs/custom-sources.md) add more, one per source.

Each collector runs on its own interval, set with `interval` in its `[collectors.*]` section. If
you leave it out, the collector uses `[daemon] default_interval` (5s). The minimum is 100ms.
Collectors that report rates (CPU, disk I/O, network, power, per-process I/O) compute them from the
difference between two readings, so their first reading after startup is a baseline.

```toml
[collectors.cpu]
interval = "1s"

[collectors.disk]
interval = "30s"
```

## CPU

Reads `/proc/stat` and reports usage per core and for the whole CPU.

- **Metrics:** user, system, idle, iowait and steal %, per core and overall. Overall usage is
  100 − idle, so time stolen by a hypervisor counts as busy.
- **Storage:** `cpu_metrics` (overall row has `core = -1`)

## Memory

Reads `/proc/meminfo`.

- **Metrics:** total, used, available, buffers, cached, swap total and used
- **Storage:** `memory_metrics`
- **ZFS:** the kernel leaves the ARC out of `Cached` and `MemAvailable`, so bewitch adds it to cached, and the part above the ARC's minimum size (`c_min`, which ZFS gives back under memory pressure) to available. Without this, a ZFS host's ARC would show as used memory.

## Load Average

Reads the 1, 5 and 15-minute load averages from `/proc/loadavg`.

- **Storage:** `load_metrics`

## Disk

Three data sources per mount: space usage (via `statfs`), I/O rates (via `/proc/diskstats`), and SMART health (via `smartctl` or direct device access).

### Space

- **Metrics:** total, used, free bytes and used % per mount; total and free inodes
- **Storage:** `disk_metrics`
- Excluded by default: `/snap/`, `/run/`, `/etc/pve` (Proxmox VE's cluster config filesystem) and `/var/lib/docker/` (per-layer mounts of Docker's ZFS/btrfs storage drivers). Add your own with `exclude_mounts`; set `no_default_excludes = true` to drop the defaults.
- Bind mounts of a directory on a filesystem that's already listed (such as the paths systemd sandboxing creates) are hidden.

### I/O

- **Metrics:** read/write bytes per second and IOPS per mount
- Symlinked mount sources (`/dev/mapper/*` for LVM/LUKS, `/dev/disk/by-*`) are resolved to their kernel name (`dm-1`, `sda1`) before matching `/proc/diskstats`.
- ZFS datasets have no `/proc/diskstats` entry; their I/O comes from the per-dataset counters in `/proc/spl/kstat/zfs/<pool>/objset-*`. These are logical: reads served from the ARC count too.

### SMART Health

Reads SMART data per physical device (not per partition). Multiple mounts from the same disk share one SMART read. Snapshots are stored in the `smart_metrics` table every `smart_interval` (default 5m, minimum 30s), so you can chart and [alert on](@/docs/alerts.md#threshold) them. Mount sources that aren't block devices (e.g. Proxmox's `/etc/pve`, mounted from `/dev/fuse`) are skipped. For a ZFS dataset, SMART is read from every disk in its pool (found from udev's `zfs_member` labels); a mount on a multi-disk pool shows a failing disk first, otherwise the first one.

- **NVMe:** available spare %, percent used, critical warning, temperature, power-on hours, power cycles
- **SATA:** reallocated sectors, pending sectors, uncorrectable errors, temperature, power-on hours
- **Sources, in order:** `smartctl` (from `smartmontools`, if installed), then built-in readers. The fallback is per device.
- **Requires:** `CAP_SYS_RAWIO` and membership of the `disk` group (the packaged service has both)

If health data can't be read, the device is reported as SMART-unavailable rather than as a healthy all-zero reading.

#### NVMe and `CAP_SYS_ADMIN`

On recent kernels, reading the NVMe health log requires `CAP_SYS_ADMIN`. bewitchd deliberately does **not** hold that capability. Instead, a small privileged helper — `bewitch-smart.service`, run every 5 minutes by `bewitch-smart.timer` — executes `bewitchd smart-dump`, which reads each NVMe namespace and writes a JSON snapshot to `/var/lib/bewitch/smart/`. The daemon picks those up (snapshots older than `max(2 × smart_interval, 15m)` are ignored) and falls back to reading the device itself when none is available.

The helper takes no input, has no network access, can only open NVMe devices, and can write only to its snapshot directory. The Debian package and the install script enable the timer automatically; it does nothing on hosts without NVMe. For a manual install:

```sh
sudo systemctl enable --now bewitch-smart.timer
```

If you change `db_path`, either set `smart_helper_dir` to match the helper's `-out` directory or edit the helper unit.

```toml
[collectors.disk]
interval = "30s"
smart_interval = "5m"  # minimum 30s
# smart_helper_dir = "/var/lib/bewitch/smart"  # NVMe helper snapshots (default: "smart" next to db_path)
exclude_mounts = ["/boot/efi"]
```

## Network

Reads per-interface counters from `/proc/net/dev`.

- **Metrics:** receive/transmit bytes and packets per second, errors and drops per interface
- **Storage:** `network_metrics`

## ECC

Reads memory error counts from the kernel's EDAC memory controllers (`/sys/devices/system/edac/mc/`).
On machines without ECC memory there are no controllers, and the Hardware view says so instead of
reporting zero errors.

- **Metrics:** corrected and uncorrectable error counts, totalled across all memory controllers
- **Storage:** `ecc_metrics`, alertable as `ecc.corrected` and `ecc.uncorrectable`
- ECC errors are rare, so a longer interval (e.g. `60s`) is plenty

## Temperature

Reads hardware sensor temperatures from `/sys/class/hwmon/`. New sensors are picked up within a minute.

- **Metrics:** temperature in °C per sensor
- **Storage:** `temperature_metrics`
- Can be disabled via `enabled = false` in config
- Displayed in the Hardware view's Temperature sub-section

## Power

Reads RAPL energy counters and computes watts from their differences. Two sources, same zone names:

- **powercap** (`/sys/class/powercap/*/energy_uj`) when readable. Since kernel 5.10 these files are root-only (a side-channel mitigation, CVE-2020-8694), so this path is used when bewitchd runs as root.
- **perf events** (the kernel's `power` PMU, e.g. `energy-pkg`) otherwise. This is how the packaged daemon, which runs as the unprivileged `bewitch` user, reads power: it needs `CAP_PERFMON`, which the packaged service grants. Custom units need `AmbientCapabilities=CAP_PERFMON` (or `kernel.perf_event_paranoid <= 0`).

If RAPL counters exist but neither source is readable, the daemon logs a warning saying why. VMs and most ARM boards have no RAPL; the collector stays silent there.

- **Metrics:** watts per power zone (package, core, uncore, DRAM)
- **Storage:** `power_metrics`
- Can be disabled via `enabled = false` in config

## GPU

Monitors GPU utilization, clock, power, memory and temperature. Three backends, detected at
startup; any combination can be active at once. With no supported GPU, the GPU section is
empty.

| GPU | Source | Requires |
| --- | --- | --- |
| Intel iGPU | `intel_gpu_top -J`, kept running | `intel-gpu-tools` package, `CAP_PERFMON` |
| NVIDIA | `nvidia-smi` queries (10s timeout) | NVIDIA driver with `nvidia-smi` |
| AMD | `amdgpu` sysfs (`/sys/class/drm/card*/device/`) | Nothing extra |

- **Intel:** utilization (busiest engine), clock, power. Memory isn't reported (it's shared system memory). The first reading is a baseline.
- **NVIDIA:** utilization, memory used/total, temperature, power, clock.
- **AMD:** utilization, VRAM used/total, temperature, power, clock, read straight from the driver with no extra tools.
- **Storage:** `gpu_metrics`
- Can be disabled via `enabled = false` in config

```toml
[collectors.gpu]
# interval = "5s"
# enabled = true
```

## Process

Lists every process on the system each interval: PID, name, state, CPU %, memory (RSS) and
threads. This is cheap, so nothing is missed.

The busiest processes (top `max_processes`, default 100) plus any pinned ones are tracked in
**full**: command line, user, open file descriptors, a memory breakdown, and disk and network I/O.
Only these are stored in the database (`process_metrics` and `process_info`).

### Disk I/O per process

Read and write bytes per second, from `/proc/[pid]/io` (actual storage I/O, not page cache).
Reading other users' processes needs `CAP_SYS_PTRACE`, which the packaged service grants. Without
it, those processes show zero.

### Network I/O per process

Receive and transmit bytes per second, measured with eBPF. It covers **TCP only** (not UDP) and
needs:

- kernel 5.5 or newer with BTF (`/sys/kernel/btf/vmlinux`), which most current distributions ship
- `CAP_BPF` and `CAP_PERFMON` (the packaged service grants both)
- the daemon running in the host's PID namespace (a systemd service does; in a container, use `--pid=host`)

If any of these is missing, the daemon logs a warning and network I/O reads zero; everything else
keeps working. To turn it off and load no eBPF programs at all:

```toml
[collectors.process]
network_io = false
```

### Process pinning

Pinned processes always get full detail, even when they aren't among the busiest. Use this for
important services that are usually idle. Patterns are globs matched against the process name, or against the full command
line if no name matches.

```toml
[collectors.process]
max_processes = 100
pinned = ["nginx*", "postgres", "redis-server"]
```

You can also pin processes in the TUI with `*`. TUI pins are saved on the daemon and persist across
restarts. Processes named in [process alert rules](@/docs/alerts.md#process-down) are tracked in
full automatically.

## Failures and Backoff

If a collector fails (a missing file, a hung tool), it backs off: it skips 1, 2, 4… intervals
before retrying, up to 64 intervals, and resumes its normal schedule after the first success. The
first error is logged, later ones with the attempt count, and recovery is logged too. One failing
collector doesn't hold up the others.

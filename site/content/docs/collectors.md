+++
title = "Collectors"
description = "The nine collectors — CPU, memory, disk, network, ECC, temperature, power, GPU, process — and how to tune their intervals."
weight = 30
+++

Bewitch has 9 metric collectors. All implement the `Collector` interface with `Name()` and `Collect()` methods. Collectors run in parallel via goroutines on each tick. The daemon uses a GCD-based tick scheduler to fire each collector at its configured interval.

## CPU

Reads per-core CPU usage from `/proc/stat`. Computes delta percentages between samples. The first sample after startup is discarded (needs a baseline).

- **Metrics:** per-core usage %, aggregate %
- **Storage:** `cpu_metrics` table
- **Default interval:** inherits `default_interval` (5s)

## Memory

Reads `/proc/meminfo` for total, free, available, buffers, cached, and swap. Computes used bytes and used percentage.

- **Metrics:** total, used, free, available, buffers, cached, swap (bytes + percentages)
- **Storage:** `memory_metrics` table

## Disk

Three data sources per mount: space usage (via `statfs`), I/O rates (via `/proc/diskstats`), and SMART health (via `smartctl` or direct device access).

### Space

- **Metrics:** total, used, free bytes; used percentage per mount
- Mount filtering: `/snap/` and `/run/` excluded by default

### I/O

- **Metrics:** read/write bytes per second per device
- Delta-based: keeps previous reading, computes rate. First sample discarded.

### SMART Health

Reads SMART data per physical device (not per partition). Multiple mounts from the same disk share one SMART read. SMART data is **live-only** — not stored in the database since it changes slowly.

- **NVMe:** available spare %, percent used, critical warning, temperature, power-on hours, power cycles
- **SATA:** reallocated sectors, pending sectors, uncorrectable errors, temperature, power-on hours
- **Fallback chain:** smartctl (preferred) → smart.go library → direct SAT passthrough
- **Requires:** `CAP_SYS_RAWIO` capability (configured by Debian package)

```toml
[collectors.disk]
interval = "30s"
smart_interval = "5m"  # min 30s, "0" to disable
exclude_mounts = ["/boot/efi"]
```

## Network

Reads per-interface bytes from `/proc/net/dev`. Computes RX/TX bytes per second. Delta-based with first sample discarded.

- **Metrics:** rx_bytes/sec, tx_bytes/sec per interface
- **Storage:** `network_metrics` table with dimension IDs for interface names

## ECC

Reads ECC memory error counts from `/sys/devices/system/edac/`. Live-only data — not stored in DB. Useful for servers with ECC memory.

- **Metrics:** correctable and uncorrectable error counts per DIMM
- **Default interval:** 60s (ECC errors change very infrequently)

## Temperature

Reads hardware sensor temperatures from `/sys/class/hwmon/`. Caches sensor paths and refreshes every 60 seconds to avoid expensive glob operations.

- **Metrics:** temperature in °C per sensor
- **Storage:** `temperature_metrics` table with dimension IDs for sensor names
- Can be disabled via `enabled = false` in config
- Displayed in the Hardware tab's Temperature sub-section

## Power

Reads power consumption from Linux powercap/RAPL zones at `/sys/class/powercap/`. Delta-based, computes watts from energy counter differences. Caches zone paths (60s refresh).

- **Metrics:** watts per power zone (package, core, uncore, DRAM)
- **Storage:** `power_metrics` table with dimension IDs for zone names
- Can be disabled via `enabled = false` in config

## GPU

Monitors GPU utilization, frequency, power, and memory. Supports Intel iGPUs via `intel_gpu_top` (long-lived JSON subprocess) and NVIDIA GPUs via `nvidia-smi` (point-in-time CSV queries). Both backends auto-detect tool availability at startup; if neither is found, the collector produces empty samples.

### Intel iGPU

- Runs `intel_gpu_top -J` as a persistent subprocess streaming JSON
- Detects i915/xe driver via `/sys/class/drm/`
- Utilization = max engine busy % (Render/3D, Video, etc.)
- First sample discarded (needs prior period for deltas)
- **Requires:** `CAP_PERFMON` capability and `intel-gpu-tools` package

### NVIDIA

- Runs `nvidia-smi --query-gpu=... --format=csv` with 10s timeout
- Reports utilization, memory used/total, temperature, power, clock speed
- **Requires:** NVIDIA driver with `nvidia-smi`

- **Metrics:** utilization %, frequency MHz, power watts, memory used/total (NVIDIA), temperature (NVIDIA)
- **Storage:** `gpu_metrics` table with dimension IDs for GPU names
- Can be disabled via `enabled = false` in config
- Multi-vendor: Intel and NVIDIA backends can be active simultaneously

```toml
[collectors.gpu]
# interval = "5s"
# enabled = true  # Intel iGPU via intel_gpu_top, NVIDIA via nvidia-smi
```

## Process

Two-phase collection. Phase 1 cheaply scans all `/proc/[pid]/stat` files. Phase 2 enriches the top N processes (by CPU/memory) plus pinned processes with expensive data.

### Phase 1 (all processes)

- PID, name, state, CPU%, RSS, thread count
- Very fast — reads a single file per process

### Phase 2 (enriched processes)

- Command line, UID, FD count, detailed memory breakdown
- Reads `/proc/[pid]/cmdline`, `/proc/[pid]/status`, `/proc/[pid]/fd`
- Default: top 100 processes enriched

### Process pinning

Pinned processes always receive Phase 2 enrichment regardless of ranking. Useful for monitoring low-resource but critical services.

```toml
[collectors.process]
max_processes = 100
pinned = ["nginx*", "postgres", "redis-server"]
```

Pins can also be set interactively in the TUI with the `*` key. TUI pins persist in the daemon's preferences database across restarts.

## Collector Backoff

When a collector's `Collect()` returns an error, consecutive failures trigger exponential backoff. The collector skips `2^(n-1)` intervals (capped at 64x) before retrying. On success, the failure count resets immediately. First error is always logged; subsequent errors include attempt count and backoff duration.

## Parallel Collection

Collectors due on each tick run concurrently, reducing total cycle time. The API cache is updated immediately after collection, then samples are written to the database asynchronously.

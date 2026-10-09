<p align="center">
  <img src="site/static/witch.png" width="140" alt="">
</p>

<h1 align="center">bewitch</h1>

<p align="center">A system monitor for Linux homelabs, with a terminal UI.</p>

<p align="center">
  <a href="https://github.com/duggan/bewitch/actions/workflows/test.yml"><img src="https://github.com/duggan/bewitch/actions/workflows/test.yml/badge.svg" alt="Test"></a>
  <a href="https://github.com/duggan/bewitch/releases/latest"><img src="https://img.shields.io/github/v/release/duggan/bewitch?color=ff6ec7" alt="Latest release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-bb86fc" alt="License: Apache 2.0"></a>
</p>

![The bewitch dashboard](site/static/screenshots/dashboard.png)

bewitch monitors homelab servers, Raspberry Pis and VPSes. It has two binaries:

- `bewitchd`, a daemon that collects metrics from `/proc`, `/sys` and the hardware, stores them in
  a local DuckDB database, and evaluates alert rules.
- `bewitch`, a client with a terminal UI (live views, history charts, hardware health, alerts), a
  SQL console, and maintenance commands.

Each machine runs its own daemon; there is no central server. The packaged daemon runs as an
unprivileged, sandboxed systemd service and caps DuckDB's memory use at 512 MB by default.

## Install

```sh
curl -fsSL https://bewitch.dev/install.sh | sudo sh
```

On Debian and Ubuntu this adds the signed APT repository, so updates arrive with `apt upgrade`.
On other distributions it installs a prebuilt binary and a systemd service. Builds are available
for amd64 and arm64. See [Installation](https://bewitch.dev/docs/installation/) for the manual
APT setup, the dev channel, and tarballs.

## Use

```sh
bewitch                       # the TUI: 1–9 switch views, < > change the time range
bewitch repl                  # SQL over your own metrics
bewitch -addr myhost:9119     # a remote daemon, over TLS with fingerprint pinning
```

## What it does

- **System metrics:** CPU (per core, including steal), memory, disk space and I/O,
  network, temperatures, power (Intel/AMD RAPL), GPUs (Intel, NVIDIA, AMD), ECC memory, and every
  process, with per-process disk and network I/O.
- **Disk health:** SMART data for SATA, NVMe and ZFS pool disks, stored over time so you can
  track wear and alert on reallocated sectors, errors or failed health checks.
- **History:** every sample is stored, and the TUI charts any time range. Optional retention
  pruning and Parquet archival keep the database small.
- **Alerts:** threshold, predictive (a disk projected to reach 90% within 48 hours), variance
  and process rules, created in the TUI. Notifications go to email, a shell command, or Discord,
  Telegram, ntfy, Slack and other services, with a second notification when an alert resolves.
  A built-in alert fires if metric collection stops.
- **Custom sources:** poll Pi-hole, Home Assistant, Docker, Proxmox VE or any JSON HTTP API and
  chart the values. Sources are configured in TOML.
- **SQL and export:** a REPL over all stored data, export to CSV, Parquet or JSON, and a
  Prometheus `/metrics` endpoint.
- **Remote access:** the TUI and REPL can connect to a daemon on another machine over TLS, with
  certificate fingerprint pinning and optional token authentication.

## Screenshots

| Processes | Hardware |
| --- | --- |
| ![Process view](site/static/screenshots/process.png) | ![Hardware view](site/static/screenshots/hardware.png) |
| **Alerts** | **Services** |
| ![Alerts view](site/static/screenshots/alerts.png) | ![Services view](site/static/screenshots/services.png) |
| **CPU** | **Disk** |
| ![CPU view](site/static/screenshots/cpu.png) | ![Disk view](site/static/screenshots/disk.png) |

## Documentation

Full documentation is at **[bewitch.dev/docs](https://bewitch.dev/docs/)**:

- [Configuration](https://bewitch.dev/docs/configuration/) and [Collectors](https://bewitch.dev/docs/collectors/)
- [TUI guide](https://bewitch.dev/docs/tui/) and [Alerts](https://bewitch.dev/docs/alerts/)
- [Custom sources](https://bewitch.dev/docs/custom-sources/) and [Running on Proxmox VE](https://bewitch.dev/docs/proxmox/)
- [SQL REPL](https://bewitch.dev/docs/repl/), [Storage & archival](https://bewitch.dev/docs/archival/) and the [API](https://bewitch.dev/docs/api/)
- [Remote access](https://bewitch.dev/docs/remote-access/)

## Development

bewitch targets Linux, but builds and runs on macOS too: mock mode generates synthetic data for
every view, so you can work on the TUI anywhere. You need Go (see `go.mod`) and a C toolchain,
because DuckDB is linked through cgo.

```sh
make build              # bin/bewitchd and bin/bewitch
make test               # unit tests (make test-integration for the DuckDB ones)
```

To run against mock data, use a config like this:

```toml
[daemon]
mock = true
socket = "/tmp/bewitch.sock"
db_path = "/tmp/bewitch.duckdb"
```

```sh
bin/bewitchd -config dev.toml &
bin/bewitch -config dev.toml
```

[CLAUDE.md](CLAUDE.md) has detailed architecture notes for contributors.

## License

[Apache 2.0](LICENSE)

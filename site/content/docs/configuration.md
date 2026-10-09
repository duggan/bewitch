+++
title = "Configuration"
description = "Every TOML option, what it does, and the defaults you can usually leave alone."
weight = 20

[extra]
group = "Get started"
+++

Bewitch uses a TOML configuration file. Both `bewitchd` and `bewitch` accept `-config <path>`.
The default location is `/etc/bewitch.toml`. The full annotated example ships as
`bewitch.example.toml` (installed to `/usr/share/bewitch/` by the Debian package).

## File Permissions

The config file can hold secrets: `auth_token`, SMTP passwords, Shoutrrr URLs and custom-source
credentials. The Debian package and the install script install it as `root:bewitch`, mode `0640`,
so only root and the daemon can read it. If you create it by hand:

```bash
sudo chown root:bewitch /etc/bewitch.toml
sudo chmod 640 /etc/bewitch.toml
```

## Client Configuration

The `bewitch` TUI and CLI are run by ordinary users, who usually can't read `/etc/bewitch.toml`.
They don't need to: the client only needs the socket path, which defaults correctly. It looks for
config in this order:

1. The file given with `-config` (it must be readable).
2. `~/.config/bewitch/config.toml`, if it exists.
3. `/etc/bewitch.toml`, if you can read it.
4. Built-in defaults.

Put client-only settings in `~/.config/bewitch/config.toml`: a custom `socket`, the `auth_token`
to use with `-addr`, and `[tui]` options.

## Daemon Settings

```toml
[daemon]
# socket = "/run/bewitch/bewitch.sock"  # Unix socket path
# listen = ":9119"          # TCP listener for remote access (empty = disabled)
db_path = "/var/lib/bewitch/bewitch.duckdb"
# log_level = "info"        # debug, info, warn, error
# default_interval = "5s"   # collection interval for collectors without their own (min 100ms)
# db_memory_limit = "512MB" # cap on DuckDB's working memory; big queries spill to disk
# export_dir = "/var/lib/bewitch"  # where export and snapshot files may be written (default: the db_path directory)
# sources_dir = "/etc/sources.d"   # drop-in custom-source files (default: sources.d next to the config file)
# mock = false              # synthetic data for macOS TUI development
```

`db_memory_limit` defaults to `512MB` rather than DuckDB's own default of about 80% of RAM. Large
queries, exports and archive runs spill to a `duckdb_tmp` directory next to the database instead.
Raise it on a big host if you run heavy queries.

`export_dir` confines the files that `bewitch snapshot`, the REPL's `.export` and the export API
can write: paths must be absolute, inside this directory, and must not already exist.

### Data management

```toml
[daemon]
# retention = "30d"           # delete metrics older than this (empty = keep forever)
# prune_interval = "1h"       # how often to run pruning (requires retention)
# compaction_interval = "7d"  # full DB rebuild interval (empty = manual only)
# checkpoint_threshold = "16MB"  # DuckDB WAL auto-checkpoint size
# checkpoint_interval = "5m"    # forced checkpoint for crash safety (empty = disabled)
```

### Parquet archival

```toml
[daemon]
# archive_threshold = "7d"   # archive data older than this to Parquet (empty = disabled)
# archive_interval = "6h"    # how often to run archive
# archive_path = "/var/lib/bewitch/archive"  # Parquet output directory (default: "archive" next to db_path)
```

See [Storage & Archival](@/docs/archival.md).

### TLS and authentication

```toml
[daemon]
# listen = ":9119"           # must be set to enable TCP
# tls_cert = "/path/cert.pem"  # custom cert (empty = auto-generate)
# tls_key = "/path/key.pem"    # custom key (empty = auto-generate)
# tls_disabled = false          # set true for plain TCP (not recommended)
# auth_token = "my-secret"     # bearer token for TCP clients
```

With `tls_disabled = true`, an `auth_token` is **required**: the daemon refuses to start with a
plain-TCP listener and no token. With TLS and no token, it starts but logs a warning. See
[Remote Access](@/docs/remote-access.md).

## Alert Settings

Alert rules are managed in the TUI (Alerts view, press `n`), not in the config file. The config
file sets the evaluation interval and the notification channels. See [Alerts](@/docs/alerts.md).

```toml
[alerts]
evaluation_interval = "10s"  # how often the alert engine evaluates rules

# Discord, Telegram, ntfy, Slack, Gotify, webhooks… one URL each (secrets!)
# shoutrrr_urls = [
#   "discord://token@channel-id",
#   "ntfy://ntfy.sh/my-homelab-topic",
# ]

# Local mail system (Postfix, Exim, sendmail, msmtp)
# [[alerts.email]]
# use_mail_cmd = true
# to = ["admin@example.com"]
# from = "bewitch@myserver.local"  # optional

# Email via SMTP
# [[alerts.email]]
# smtp_host = "smtp.example.com"
# smtp_port = 587
# username = "alerts@example.com"
# password = "app-password"
# from = "alerts@example.com"
# to = ["admin@example.com"]
# starttls = true  # false for implicit TLS on port 465

# Shell command
# [[alerts.commands]]
# cmd = "/usr/local/bin/alert-handler"
# receives BEWITCH_RULE, BEWITCH_SEVERITY, BEWITCH_MESSAGE, BEWITCH_STATUS (firing|resolved), BEWITCH_TIMESTAMP
```

## TUI Settings

```toml
[tui]
refresh_interval = "2s"
# history_ranges = ["1h", "6h", "24h", "7d", "30d"]

# Screenshot export (the x key)
# [tui.capture]
# directory = "~/screenshots"  # default: home directory
# dpi = 144                    # 72 = 1x, 144 = 2x (default), 216 = 3x
# compression = "best"         # "default", "best", "none"
# background = "#1A1A2E"
# foreground = "#F8F8F2"
```

## Collector Settings

Each collector has its own section with an `interval` field. If omitted, the collector uses
`default_interval` from the `[daemon]` section (default 5s, minimum 100ms). See
[Collectors](@/docs/collectors.md) for what each one reads.

```toml
[collectors.cpu]
# interval = "1s"

[collectors.memory]
# interval = "5s"

[collectors.load]
# interval = "5s"

[collectors.disk]
# interval = "30s"
# smart_interval = "5m"         # how often to read SMART (min 30s)
# smart_helper_dir = "/var/lib/bewitch/smart"  # NVMe SMART helper snapshots (default: "smart" next to db_path)
# exclude_mounts = ["/boot/efi"]  # additional mount exclusions (prefix match)
# no_default_excludes = false    # true to disable defaults (/snap/, /run/, /etc/pve, /var/lib/docker/)

[collectors.network]
# interval = "5s"

[collectors.ecc]
# interval = "60s"

[collectors.temperature]
# interval = "5s"
# enabled = true   # false to disable

[collectors.power]
# interval = "5s"
# enabled = true   # false to disable

[collectors.gpu]
# interval = "5s"
# enabled = true   # Intel (intel_gpu_top), NVIDIA (nvidia-smi), AMD (sysfs)

[collectors.process]
# interval = "5s"
# max_processes = 100   # processes tracked in full each cycle
# pinned = ["nginx*", "postgres", "redis-server"]
# network_io = true     # per-process TCP I/O via eBPF; false loads no BPF programs
```

Custom HTTP sources are configured with top-level `[[custom_source]]` blocks or drop-in files; see
[Custom Sources](@/docs/custom-sources.md).

## macOS Mock Mode

For TUI development on macOS, enable mock mode to generate synthetic metrics from a simulated server:

```toml
[daemon]
mock = true
socket = "/tmp/bewitch.sock"
db_path = "/tmp/bewitch.duckdb"
```

```bash
make build
bin/bewitchd -config dev.toml &
bin/bewitch -config dev.toml
```

Mock mode simulates an 8-core server with 32 GB RAM, two disks, two network interfaces, temperature sensors,
power zones, two GPUs (Intel + NVIDIA), two custom sources, and ~65 processes. Data uses smooth sine waves with jitter for a realistic feel.

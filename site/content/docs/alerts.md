+++
title = "Alerts"
description = "Threshold, predictive, and variance rules, delivered by email or shell command."
weight = 50
+++

Alert rules are created and managed from the TUI (Alerts view, press `n`). Rules are stored in the database and evaluated by the daemon on every evaluation cycle.

## Alert Types

### Threshold

Fires when a metric average exceeds a value for a sustained duration.

| Metric | Description |
| --- | --- |
| `cpu.aggregate` | Aggregate CPU usage % |
| `memory.used_pct` | Memory usage % |
| `disk.used_pct` | Disk usage % (per mount) |
| `network.rx` | Network receive bytes/sec (per interface) |
| `network.tx` | Network transmit bytes/sec (per interface) |
| `temperature.sensor` | Temperature in °C (per sensor) |
| `gpu.utilization` | GPU utilization % (per device) |
| `gpu.temperature` | GPU temperature in °C (per device) |
| `gpu.power` | GPU power draw in watts (per device) |

### Predictive

Uses linear regression to predict when a metric will breach a threshold within a given timeframe.

- `disk.used_pct` — predicts disk fill within 24h, 3d, or 7d

### Variance

Fires when memory usage changes exceed a delta threshold a certain number of times within a window. Useful for detecting memory thrashing or crash loops.

- `memory.variance` — counts memory usage spikes exceeding N% within a time window

## Debouncing

The alert engine won't re-fire the same rule if an unacknowledged alert exists within 3x the evaluation interval. This prevents alert storms for persistent conditions.

## Notification Channels

Alerts can be delivered via email or shell command. All are configured in the TOML config file under `[alerts]`.

### Email (local mail command)

Send email alerts using the local `mail` command (postfix/sendmail). No SMTP configuration needed.

```toml
[[alerts.email]]
use_mail_cmd = true
to = ["admin@example.com"]
from = "bewitch@myserver.local"  # optional, uses system default if omitted
```

### Email (SMTP)

Send email alerts via a remote SMTP server with STARTTLS or implicit TLS.

```toml
[[alerts.email]]
smtp_host = "smtp.example.com"
smtp_port = 587
username = "alerts@example.com"
password = "app-password"
from = "alerts@example.com"
to = ["admin@example.com", "ops@example.com"]
starttls = true  # false for implicit TLS (port 465)
```

### Command

Execute an arbitrary shell command with alert details as environment variables.

```toml
[[alerts.commands]]
cmd = "/usr/local/bin/alert-handler"
```

Available environment variables:

| Variable | Content |
| --- | --- |
| `BEWITCH_RULE` | Rule name |
| `BEWITCH_SEVERITY` | warning or critical |
| `BEWITCH_MESSAGE` | Alert message |
| `BEWITCH_TIMESTAMP` | ISO 8601 timestamp |

Commands run with a 10-second timeout.

## Testing Notifications

Press `t` on the Alerts view to send a test notification through all configured channels. This triggers the `POST /api/test-notifications` endpoint which sends synchronously (blocks until all channels respond).

## Managing Rules via API

Rules can also be managed programmatically. See the [API Reference](/docs/api) for the alert-rules endpoints.

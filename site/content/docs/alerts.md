+++
title = "Alerts"
description = "Threshold, predictive, variance and process rules, delivered by email, chat, push or shell command."
weight = 40

[extra]
group = "Use it"
+++

You create and manage alert rules in the TUI: open the Alerts view and press `n` (or `e` to edit the
selected rule). Rules are stored in the database, and the daemon evaluates every enabled rule each
`evaluation_interval` (default `10s`). Each rule has a severity of `warning` or `critical`.

## Rule Types

| Type | Fires when |
| --- | --- |
| [Threshold](#threshold) | A metric's average, maximum or minimum over a window crosses a value |
| [Predictive](#predictive) | A disk is projected to reach a fill level within a time horizon |
| [Variance](#variance) | Memory usage swings sharply too many times within a window |
| [Process down](#process-down) | Fewer than N instances of a process are running |
| [Process thrashing](#process-thrashing) | A process restarts too many times within a window |

The create form only offers the hardware categories your host actually reports: SMART appears
when a disk has SMART data, GPU when a GPU is detected, temperature when sensors exist, and ECC
when the machine has ECC memory controllers.

### Threshold

Compares a metric, aggregated over the rule's window, against a value with `>`, `>=`, `<` or `<=`.

| Metric | Description |
| --- | --- |
| `cpu.aggregate` | Overall CPU usage % (100 − idle, so steal, nice and IRQ time count) |
| `cpu.steal` | CPU time stolen by the hypervisor % (VPS contention) |
| `memory.used_pct` | Memory usage % |
| `disk.used_pct` | Disk usage % (per mount) |
| `network.rx` | Receive bytes/sec (per interface) |
| `network.tx` | Transmit bytes/sec (per interface) |
| `temperature.sensor` | Temperature in °C (per sensor) |
| `gpu.utilization` | GPU utilization % (per GPU) |
| `gpu.temperature` | GPU temperature in °C (per GPU) |
| `gpu.power` | GPU power draw in watts (per GPU) |
| `smart.reallocated` | Reallocated sectors, worst disk |
| `smart.pending` | Pending sectors, worst disk |
| `smart.uncorrectable` | Uncorrectable errors, worst disk |
| `smart.percent_used` | NVMe wear %, worst disk |
| `smart.unhealthy` | Number of failed SMART health checks in the window |
| `ecc.uncorrectable` | Uncorrectable ECC memory errors (all controllers) |
| `ecc.corrected` | Corrected ECC memory errors (all controllers) |

The TUI form creates every metric except `gpu.temperature` and `gpu.power`, which you can create
through the [API](@/docs/api.md).

**Aggregate.** For the CPU, memory, disk, network, temperature and GPU metrics, you choose how the
window is summarised:

| Aggregate | Use it to |
| --- | --- |
| `avg` (default) | Catch sustained load; a short spike averages out |
| `max` | Catch a spike anywhere in the window |
| `min` | Catch a value that stays high (or low) for the whole window |

The rule compares that one number against the threshold. "cpu.aggregate > 90 over 5m" with `avg`
means the 5-minute *average* exceeded 90%, not that CPU stayed above 90% for 5 minutes. The alert
message says which aggregate was used, e.g. `cpu.aggregate avg 91.0 > 90.0 over 5m`.

The SMART and ECC metrics ignore the aggregate setting. Their counters only grow, so they always
use the worst (`max`) value in the window; `smart.unhealthy` is a count. A typical rule is
`smart.reallocated > 0` or `ecc.uncorrectable > 0`.

### Predictive

Fits a linear trend to a mount's `disk.used_pct` and fires when it is projected to reach the target
percentage within the chosen horizon (24 hours, 3 days or 7 days). The horizon is also how far back
the trend looks.

If the disk is already at or above the target, the rule fires straight away with
`… already at 95% (target 90%)` rather than waiting for a trend.

### Variance

Counts how many times memory usage changed by at least a delta (in percentage points) between
consecutive samples, and fires when the count reaches a minimum within the window. Use it to spot
memory thrashing or crash-looping services. Variance rules only support memory
(`memory.variance`).

### Process down

Fires when fewer than a minimum number of instances of a process are running. Match by exact
process name, or by a glob on the full command line (e.g. `*/myapp*`).

With a check duration set, the rule fires only if the process was below the minimum in **every**
sample over that duration, so a quick restart or one missed sample doesn't trigger it.

### Process thrashing

Fires when a process has started at least N times (counting new PIDs) within a window, the sign of
a crash loop. A process that dies faster than the process collector's interval can be missed.

You can create either process rule from the Process view: select a process and press `a`. Processes
named in process rules are always tracked in full, even when they aren't among the busiest.

## Firing and Recovery

A rule fires once when it starts breaching and stays a single **active** alert while the condition
holds. It doesn't re-fire every cycle, and acknowledging it doesn't make it fire again. When the
condition clears, the alert is marked **resolved** and a recovery notification is sent:

- Email subjects read `[bewitch] RESOLVED warning: high-cpu`.
- Shoutrrr titles read `[bewitch] RESOLVED …`.
- Commands receive `BEWITCH_STATUS=resolved` (`firing` otherwise).

Deleting a rule also deletes its fired alerts. Renaming a rule keeps its alerts attached to it.

### Collection stalled

bewitch has one built-in rule, `collection-stalled`. It fires a critical alert when the newest CPU
sample is older than 2 minutes (or 12 × `evaluation_interval`, whichever is longer), which means
collection has stopped even though the daemon is still running. It resolves when data flows again.
If the whole daemon dies, it can't alert, so monitor the service itself from outside as well.

## Notification Channels

Configure notification channels in the `[alerts]` section of the config file. Every firing alert,
recovery and `collection-stalled` alert goes to every configured channel.

### Email (local mail command)

Send email alerts through the host's own mail system (Postfix, Exim, sendmail, or a relay like msmtp). No SMTP configuration needed.

```toml
[[alerts.email]]
use_mail_cmd = true
to = ["admin@example.com"]
from = "bewitch@myserver.local"  # optional; defaults to bewitch@<hostname>
```

**Under the packaged service** (which sets `NoNewPrivileges`), bewitch can't use the setgid/setuid helpers that Postfix, Exim and sendmail rely on for local submission (Postfix's `postdrop` fails with `Permission denied`). So when it detects `NoNewPrivileges`, bewitch hands the message to the local MTA over SMTP on `127.0.0.1:25` instead, without TLS or authentication since it never leaves the host. Your MTA must therefore be **running and listening on loopback**: Postfix does by default (`inet_interfaces` includes loopback and `mynetworks` includes `127.0.0.0/8`). If nothing is listening there, bewitch falls back to running `mail`, which works for unprivileged relays such as msmtp. The daemon logs which path it used the first time it sends.

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

### Shoutrrr (Discord, Telegram, ntfy, Slack, …)

[Shoutrrr](https://containrrr.dev/shoutrrr/services/) URLs reach Discord, Telegram, ntfy, Slack,
Pushover, Gotify, generic webhooks and more, each from a single URL:

```toml
[alerts]
shoutrrr_urls = [
  "discord://token@channel-id",
  "ntfy://ntfy.sh/my-homelab-topic",
  "telegram://token@telegram?chats=12345",
]
```

`shoutrrr_urls` is a list of strings under `[alerts]`, not a `[[…]]` table. The URLs contain
tokens, so treat them as secrets: keep the config file `0640 root:bewitch`. bewitch logs and
reports them by scheme only (`discord://***`).

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
| `BEWITCH_SEVERITY` | `warning` or `critical` |
| `BEWITCH_MESSAGE` | Alert message |
| `BEWITCH_STATUS` | `firing` or `resolved` |
| `BEWITCH_TIMESTAMP` | RFC 3339 timestamp (UTC) |

Commands run with a 10-second timeout. On timeout, the command and any processes it started are
killed.

## Testing Notifications

Press `t` in the Alerts view to send a test alert through every configured channel. The test is a
fixed message (rule `test`, severity `info`, "Test notification from bewitch"). The result for each
channel, including errors and latency, appears in the view; press `c` on the rules panel to dismiss
it. The same test is available as `POST /api/test-notifications`.

## Managing Rules via API

You can also manage rules programmatically. See the [API Reference](@/docs/api.md) for the
alert-rules endpoints.

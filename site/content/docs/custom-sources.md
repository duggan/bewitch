+++
title = "Custom Sources"
description = "Point bewitch at a local service's HTTP API and chart its numbers alongside everything else — no Go required."
weight = 35
+++

Bewitch monitors the host. Custom sources let it monitor the *things running on* the host:
Pi-hole's blocked queries, Home Assistant's sensors, Docker's container count, Homebridge's
status — anything with a local HTTP API that returns JSON.

You declare a source in TOML. The daemon polls it, pulls out the fields you name, and treats
the numbers as first-class metrics: charted over time, scrapeable from `/metrics`, and shown
in a dedicated **Services** tab in the TUI. No plugin to compile, no Go to write.

## A first source

```toml
[[custom_source]]
name      = "pihole"
interval  = "10s"
base_url  = "http://pi.hole"

  [custom_source.request]
  path = "/admin/api.php"

  [[custom_source.metric]]
  name = "queries"
  path = "dns_queries_today"
  unit = "count"

  [[custom_source.metric]]
  name = "blocked"
  path = "ads_blocked_today"
  unit = "count"

  [[custom_source.status]]
  label = "Blocking"
  path  = "status"
```

That endpoint returns `{"dns_queries_today": 48213, "ads_blocked_today": 9102, "status": "enabled", ...}`.
Bewitch stores `queries` and `blocked` as time-series, and shows `Blocking: enabled` live on the
Services tab. Done.

## Where definitions live

Two places, merged at startup:

- **Inline** in `bewitch.toml` as `[[custom_source]]` blocks (like the example above).
- **Drop-in files** under a `sources.d/` directory — one `*.toml` per service, each containing
  its own `[[custom_source]]` blocks. The directory defaults to `sources.d/` next to your config
  file; override it with `[daemon] sources_dir`.

Drop-in files make sources shareable — a `pihole.toml` you can hand to someone else, or keep
your secrets out of the main config. If a drop-in defines a source with the same `name` as an
inline one, the drop-in wins (and the daemon logs that it did).

## Extracting values

`path` is a [gjson](https://github.com/tidwall/gjson) path into the JSON response. That means more
than dotted keys — array indices and filters work too:

```toml
path = "containers.0.State"               # first element of an array
path = "clients.#"                        # length of an array
path = "containers.#(State==\"running\").Id" # first match of a query
```

A field whose path isn't found is simply skipped — one missing key doesn't sink the whole poll.
If *none* of the configured paths are found, the poll is treated as an error (wrong endpoint, or
the API's shape changed) and the source backs off and retries, same as any other collector.

## Metrics vs. status

Each source declares two kinds of fields:

- `[[custom_source.metric]]` — a **number**. Stored in DuckDB, charted, and exported to Prometheus.
  Has a `name` (the series key), a `path`, and a `unit`.
- `[[custom_source.status]]` — anything **non-numeric** (a version string, a connection state).
  Shown live on the Services tab but never stored. Has a `label`, a `path`, and optional `badges`.

`unit` is a display hint — it controls how the value is formatted in the TUI and on the chart axis:

| unit       | formatted as            |
|------------|-------------------------|
| `bytes`    | `1.0M`, `4.2G`          |
| `bits`     | `25.3Mb`                |
| `percent`  | `42.5%`                 |
| `count`    | `7`                     |
| `duration` | `1m30s` (value in seconds) |
| `raw`      | the bare number         |

`badges` map an exact status value to a colour, so a bad state stands out:

```toml
  [[custom_source.status]]
  label = "Health"
  path  = "health"
  [custom_source.status.badges]
  ok       = "ok"      # green
  degraded = "warn"    # amber
  down     = "crit"    # red
```

## Authentication

Most local APIs want a token or login. The `[custom_source.auth]` block covers the common cases:

```toml
  [custom_source.auth]
  type = "bearer"          # Authorization: Bearer <token>
  token = "..."

  # type = "basic"         # HTTP basic auth
  # username = "..."
  # password = "..."

  # type = "header"        # an arbitrary header (e.g. a session cookie)
  # header_name  = "Cookie"
  # header_value = "SID=..."
```

You can also set arbitrary request headers under `[custom_source.request] headers = { ... }`.
Secrets stay on the daemon host — they're never written to logs and never returned by the API.

## Talking to Docker (unix socket)

Docker has no TCP port by default; it listens on a unix socket. Set `unix_socket` and bewitch dials
that instead of a host:

```toml
[[custom_source]]
name        = "docker"
interval    = "15s"
unix_socket = "/var/run/docker.sock"
base_url    = "http://unix"        # placeholder host; the socket is dialed instead

  [custom_source.request]
  path = "/info"

  [[custom_source.metric]]
  name = "containers_running"
  path = "ContainersRunning"
  unit = "count"

  [[custom_source.metric]]
  name = "images"
  path = "Images"
  unit = "count"

  [[custom_source.status]]
  label = "Server"
  path  = "ServerVersion"
```

The daemon's user needs read access to the socket (add `bewitch` to the `docker` group, or point
it at a read-only proxy).

## The Services tab

Configure at least one source and a **Services** tab appears in the TUI (it's hidden otherwise).
Each source is a sub-section — cycle them with `tab` / `shift+tab`. You get the live status strip,
the current value of each metric, and a history chart for the selected metric. Pick which metric to
chart with `↑` / `↓`; change the time range with `<` / `>` and `r`, same as every other chart.

## Prometheus and SQL

Custom metrics show up everywhere host metrics do:

```
# /metrics
bewitch_custom_value{source="pihole",metric="queries"} 48213
```

```sql
-- bewitch repl
SELECT source, metric, AVG(value)
FROM custom_metrics
WHERE ts > now() - INTERVAL 1 HOUR
GROUP BY 1, 2;
```

Status fields are deliberately *not* exported to Prometheus (string values would blow up label
cardinality) — they're live-only.

## Per-source intervals and timeouts

Each source is its own collector, so it has its own `interval` and its own failure backoff — a
flaky Home Assistant won't slow down Pi-hole. `timeout` bounds a single request and is automatically
capped below the interval, so a hung endpoint can never stall the collection cycle. A good rule of
thumb: keep `interval` at least twice `timeout`.

## A note on trust

Custom sources are operator configuration — the same trust level as an alert command. The daemon
will fetch whatever URL you give it, so point it at services you control (typically loopback).
Redirects are disabled and response bodies are capped, but bewitch deliberately *doesn't* block
private/loopback addresses: that's where Pi-hole, Home Assistant, and Docker actually live.

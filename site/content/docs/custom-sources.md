+++
title = "Custom Sources"
description = "Point bewitch at a local service's HTTP API and chart its numbers alongside everything else — no Go required."
weight = 60

[extra]
group = "Use it"
+++

Custom sources poll a service's HTTP API and record the fields you choose: Pi-hole's blocked
queries, Home Assistant's sensors, Docker's container count, Homebridge's status, or anything
else that returns JSON.

You declare a source in TOML. The daemon polls it, pulls out the fields you name, and treats
the numbers like any other metric: charted over time, scrapeable from `/metrics`, and shown
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
  file (`/etc/sources.d/` for `/etc/bewitch.toml`); override it with `[daemon] sources_dir`.

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

A field whose path isn't found is skipped, and the rest of the poll is still recorded.
If *none* of the configured paths are found, the poll is treated as an error (wrong endpoint, or
the API's shape changed) and the source backs off and retries, same as any other collector.

## Metrics vs. status

Each source declares two kinds of fields:

- `[[custom_source.metric]]` — a **number**. Stored in DuckDB, charted, and exported on bewitch's [`/metrics`](@/docs/api.md#prometheus-metrics) endpoint.
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

## Self-signed HTTPS

Homelab services often serve HTTPS with a self-signed certificate (Proxmox VE on `:8006`,
TrueNAS, OPNsense, UniFi). By default those fail verification. Add a `[custom_source.tls]` block
with **one** of:

```toml
  [custom_source.tls]
  fingerprint = "AB:CD:…"            # pin the server's certificate (SHA-256) — recommended
  # ca_file = "/etc/bewitch/my-ca.pem" # or verify against your own CA (hostname is checked)
  # insecure_skip_verify = true        # last resort: no verification (logged as a warning)
```

The fingerprint can be written `sha256:<hex>`, as plain hex, or colon-separated the way most web
UIs display it. With a pin, bewitch checks that the server presents exactly that certificate and
ignores the chain and hostname. If the pin doesn't match, the error names the fingerprint the
server *did* present. Check it against the service's own UI before pinning it; bewitch never
trusts it automatically.

## Talking to Docker

Docker's API listens on a unix socket, `/var/run/docker.sock`. Access to that socket is
root-equivalent: anything that can talk to it can start a privileged container. So the packaged
daemon, which runs as the unprivileged `bewitch` user, isn't in the `docker` group, and **adding it
is not recommended**.

Instead, run a **read-only socket proxy** that exposes only the endpoints bewitch needs, on
loopback. With [linuxserver/socket-proxy](https://docs.linuxserver.io/images/docker-socket-proxy/):

```yaml
# docker-compose.yml
services:
  socket-proxy:
    image: lscr.io/linuxserver/socket-proxy:latest
    container_name: socket-proxy
    restart: unless-stopped
    environment:
      INFO: "1"      # GET /info: everything below needs only this
      POST: "0"      # no writes
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    ports:
      - "127.0.0.1:2375:2375"   # loopback only
    read_only: true
    tmpfs:
      - /run
```

Every API section you don't enable returns `403`, as does any write. Then point the source at it:

```toml
[[custom_source]]
name     = "docker"
interval = "15s"
base_url = "http://127.0.0.1:2375"

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

If bewitchd runs as root, or otherwise has access to the socket, it can talk to the socket
directly: add `unix_socket = "/var/run/docker.sock"` and set `base_url = "http://unix"` (a
placeholder host; the socket is dialed instead). The same `unix_socket` option works for any
service that serves HTTP on a unix socket.

## The Services tab

Configure at least one source and a **Services** tab appears in the TUI (it's hidden otherwise).
Each source is a sub-section — cycle them with `tab` / `shift+tab`. You get the live status strip,
the current value of each metric, and a history chart for the selected metric. Pick which metric to
chart with `↑` / `↓`; change the time range with `<` / `>` and `r`, same as every other chart.

## Prometheus and SQL

Custom metrics are stored, charted and exported like host metrics. They can't be used in
alert rules yet.

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

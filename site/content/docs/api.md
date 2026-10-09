+++
title = "API Reference"
description = "The HTTP endpoints behind it all — metrics, history, query, export, snapshots, alerts."
weight = 95

[extra]
group = "Reference"
+++

The daemon exposes an HTTP API over its unix socket. When TCP is enabled, the same API is
available over TLS with optional bearer token authentication. All responses are JSON.

## Endpoints

{{ api_endpoints() }}

## Examples

```bash
# get daemon status
curl --unix-socket /run/bewitch/bewitch.sock \
  http://localhost/api/status
```

```bash
# get CPU metrics
curl --unix-socket /run/bewitch/bewitch.sock \
  http://localhost/api/metrics/cpu
```

```bash
# get history with time range
curl --unix-socket /run/bewitch/bewitch.sock \
  "http://localhost/api/history/cpu?start=$(date -d '1 hour ago' +%s)&end=$(date +%s)"
```

```bash
# create alert rule
curl --unix-socket /run/bewitch/bewitch.sock \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "high-cpu",
    "type": "threshold",
    "severity": "warning",
    "metric": "cpu.aggregate",
    "operator": ">",
    "value": 90,
    "duration": "5m",
    "aggregate": "avg"
  }' \
  http://localhost/api/alert-rules
```

`aggregate` is `avg` (the default), `max` or `min`; see
[threshold rules](@/docs/alerts.md#threshold). Rule names must be unique (a duplicate returns
`409`), and a rule's type can't be changed by `PUT`.

```bash
# execute SQL query
curl --unix-socket /run/bewitch/bewitch.sock \
  -H 'Content-Type: application/json' \
  -d '{"sql": "SELECT COUNT(*) as n FROM cpu_metrics"}' \
  http://localhost/api/query
```

```bash
# remote access (TCP + TLS + auth)
curl -k -H "Authorization: Bearer my-secret-token" \
  https://myserver:9119/api/status
```

Queries are read-only, single-statement, and limited to 30 seconds and 1 million rows; see
[SQL REPL restrictions](@/docs/repl.md#restrictions). Export and snapshot paths must be absolute,
inside `export_dir`, and must not already exist.

## Prometheus Metrics

`GET /metrics` (not under `/api/`) returns the current metrics in Prometheus text format:

```bash
curl --unix-socket /run/bewitch/bewitch.sock http://localhost/metrics
```

Over TCP it requires the same bearer token as the rest of the API. See
[Remote Access](@/docs/remote-access.md#prometheus) for a scrape config.

## Response Types

Timestamps are `int64` Unix nanoseconds. Arrays are always wrapped in objects. Errors return
`{"error": "message"}`.

{{ api_types() }}

## ETag Caching

Live metric endpoints include `ETag` headers. Send `If-None-Match` to receive
`304 Not Modified` when data hasn't changed.

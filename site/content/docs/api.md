+++
title = "API Reference"
description = "The HTTP endpoints behind it all — metrics, history, query, export, snapshots, alerts."
weight = 80
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
    "duration": "5m"
  }' \
  http://localhost/api/alert-rules
```

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

## Response Types

Timestamps are `int64` Unix nanoseconds. Arrays are always wrapped in objects. Errors return
`{"error": "message"}`.

{{ api_types() }}

## ETag Caching

Live metric endpoints include `ETag` headers. Send `If-None-Match` to receive
`304 Not Modified` when data hasn't changed.

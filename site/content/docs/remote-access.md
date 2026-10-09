+++
title = "Remote Access"
description = "TLS by default, trust-on-first-use fingerprint pinning, and optional bearer-token auth."
weight = 70

[extra]
group = "Use it"
+++

The daemon can listen on TCP for remote TUI, REPL, and CLI access. TCP connections use TLS by default with auto-generated self-signed certificates and SSH-style trust-on-first-use fingerprint pinning.

## Daemon Configuration

```toml
[daemon]
listen = ":9119"                  # enable TCP listener
auth_token = "my-secret-token"    # require client authentication

# Optional: provide your own certificate
# tls_cert = "/etc/bewitch/tls/cert.pem"
# tls_key = "/etc/bewitch/tls/key.pem"

# Not recommended:
# tls_disabled = true  # plain TCP without encryption
```

On first start with TCP enabled, the daemon generates a self-signed ECDSA P-256 certificate and persists it next to the database file (e.g., `/var/lib/bewitch/tls-cert.pem`). The certificate is reused across restarts so the fingerprint remains stable. The SHA-256 fingerprint is logged at startup.

## Connecting

```bash
bewitch -addr myserver:9119 -token my-secret-token
```

If you leave out `-token`, the client uses `auth_token` from its config file (see
[Client Configuration](@/docs/configuration.md#client-configuration)).

## Trust on First Use (TOFU)

On first connection, the client performs a pre-flight TLS handshake before entering the TUI and displays the server's certificate fingerprint:

```text
TLS fingerprint for myserver:9119:
  sha256:a1b2c3d4e5f6...
Trust this server? [y/N]: y
```

Accepted fingerprints are saved to `~/.config/bewitch/known_hosts` (one line per server: `addr fingerprint`). On subsequent connections, the fingerprint is verified silently.

### Fingerprint mismatch

If the server's certificate changes unexpectedly, the connection is refused:

```text
TLS: server fingerprint changed!
  Expected: sha256:a1b2c3d4...
  Got:      sha256:e5f6a7b8...
If this is expected, reconnect with -tls-reset-fingerprint to update.
```

## Client Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-addr` | `""` | Remote daemon address (e.g., `myserver:9119`) |
| `-token` | `""` | Bearer token for authentication (falls back to config) |
| `-tls` | `true` | Use TLS for TCP connections |
| `-tls-skip-verify` | `false` | Skip fingerprint verification |
| `-tls-reset-fingerprint` | `false` | Update stored fingerprint for this server |

## Authentication

When `auth_token` is set in the daemon config, every TCP request must carry the token, sent as
`Authorization: Bearer <token>`. Clients pass it with `-token` or from their config file.

What happens without a token depends on TLS:

| Listener | No `auth_token` |
| --- | --- |
| TLS (default) | Starts, with a warning: anyone who accepts the certificate can connect |
| Plain TCP (`tls_disabled = true`) | **Refuses to start**: the API would be open in the clear |

### The local socket

The unix socket is never authenticated and is world-accessible (`0666`) by design, so any local
user can run the TUI and REPL. It is **not** a privilege boundary. Instead, the risky operations
are restricted for every caller:

- SQL queries are read-only and can't read files or make network requests.
- Exports and snapshots can only write new files inside `export_dir`.
- The notification test sends a fixed message; callers can't choose its content.
- Compaction and archiving can't be triggered more than once every 30 seconds.
- The config endpoint hides notification commands and email addresses.

To limit which local users can connect, restrict the socket directory: set
`RuntimeDirectoryMode=0750` in a systemd override and add those users to the `bewitch` group.

## All Subcommands Support Remote

```bash
bewitch -addr myserver:9119 -token secret           # TUI
bewitch -addr myserver:9119 -token secret repl       # SQL REPL
bewitch -addr myserver:9119 -token secret compact    # trigger compaction
bewitch -addr myserver:9119 -token secret archive    # trigger archival
bewitch -addr myserver:9119 -token secret snapshot /tmp/remote.duckdb
```

## Prometheus

The daemon serves current metrics in Prometheus text format at `GET /metrics`, on the socket and
on the TCP listener. Over TCP it needs the same bearer token as everything else:

```yaml
# prometheus.yml
scrape_configs:
  - job_name: bewitch
    scheme: https
    authorization:
      credentials: my-secret-token
    tls_config:
      insecure_skip_verify: true   # self-signed cert; or set ca_file to your own
    static_configs:
      - targets: ["myserver:9119"]
```

Series are prefixed `bewitch_` (e.g. `bewitch_cpu_percent`, `bewitch_smart_healthy`,
`bewitch_power_watts`), with mounts, interfaces, sensors and so on as labels. Processes are exported
as aggregate counts only, and custom-source status strings aren't exported. The daemon's own health
is exported as `bewitch_self_*`.

## How Fingerprint Pinning Works

The client checks the server certificate's SHA-256 fingerprint against the one you accepted,
rather than validating a CA chain, like SSH's `known_hosts`. This is why a self-signed certificate
is safe to use once you've verified the fingerprint.

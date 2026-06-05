+++
title = "Remote Access"
description = "TLS by default, trust-on-first-use fingerprint pinning, and optional bearer-token auth."
weight = 70
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

If the daemon and client share the same config file (e.g., on the same machine), the token is read from config automatically — no `-token` flag needed.

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

When `auth_token` is set in the daemon config, all TCP connections must include the token via the `-token` flag or config file. The token is transmitted as a Bearer token in the HTTP Authorization header and compared using constant-time comparison.

Unix socket connections are **never authenticated** — filesystem permissions are sufficient. The daemon logs a warning at startup if TCP is enabled without an auth token.

## All Subcommands Support Remote

```bash
bewitch -addr myserver:9119 -token secret           # TUI
bewitch -addr myserver:9119 -token secret repl       # SQL REPL
bewitch -addr myserver:9119 -token secret compact    # trigger compaction
bewitch -addr myserver:9119 -token secret archive    # trigger archival
bewitch -addr myserver:9119 -token secret snapshot /tmp/remote.duckdb
```

## How It Works

The daemon runs separate server instances for the unix socket and TCP listener, sharing the same API. The unix socket has no auth (filesystem permissions suffice). The TCP listener applies bearer token middleware. Both are shut down gracefully on daemon exit.

The client transparently adds the Authorization header to every request. TLS fingerprint pinning verifies the server certificate's fingerprint directly, bypassing CA chain validation — similar to SSH known_hosts.

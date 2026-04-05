import type { FC } from 'hono/jsx'
import { docsBase } from '../../docs-base'
import { DocsLayout } from '../../layouts/docs'
import { CodeBlock } from '../../components/terminal-block'

export const RemoteAccessDocs: FC = () => (
  <DocsLayout title="Remote Access" active={`${docsBase}/remote-access`}>
    <p>
      The daemon can listen on TCP for remote TUI, REPL, and CLI access. TCP connections use TLS by default
      with auto-generated self-signed certificates and SSH-style trust-on-first-use fingerprint pinning.
    </p>

    <h2>Daemon Configuration</h2>
    <CodeBlock title="bewitch.toml">
{`[daemon]
listen = ":9119"                  # enable TCP listener
auth_token = "my-secret-token"    # require client authentication

# Optional: provide your own certificate
# tls_cert = "/etc/bewitch/tls/cert.pem"
# tls_key = "/etc/bewitch/tls/key.pem"

# Not recommended:
# tls_disabled = true  # plain TCP without encryption`}
    </CodeBlock>

    <p>
      On first start with TCP enabled, the daemon generates a self-signed ECDSA P-256 certificate and persists it
      next to the database file (e.g., <code>/var/lib/bewitch/tls-cert.pem</code>). The certificate is reused across
      restarts so the fingerprint remains stable. The SHA-256 fingerprint is logged at startup.
    </p>

    <h2>Connecting</h2>
    <CodeBlock>
{`bewitch -addr myserver:9119 -token my-secret-token`}
    </CodeBlock>

    <p>
      If the daemon and client share the same config file (e.g., on the same machine), the token is read from
      config automatically — no <code>-token</code> flag needed.
    </p>

    <h2>Trust on First Use (TOFU)</h2>
    <p>
      On first connection, the client performs a pre-flight TLS handshake before entering the TUI and displays
      the server's certificate fingerprint:
    </p>
    <CodeBlock title="first connection">
{`TLS fingerprint for myserver:9119:
  sha256:a1b2c3d4e5f6...
Trust this server? [y/N]: y`}
    </CodeBlock>

    <p>
      Accepted fingerprints are saved to <code>~/.config/bewitch/known_hosts</code> (one line per server:
      <code>addr fingerprint</code>). On subsequent connections, the fingerprint is verified silently.
    </p>

    <h3>Fingerprint mismatch</h3>
    <p>If the server's certificate changes unexpectedly, the connection is refused:</p>
    <CodeBlock>
{`TLS: server fingerprint changed!
  Expected: sha256:a1b2c3d4...
  Got:      sha256:e5f6a7b8...
If this is expected, reconnect with -tls-reset-fingerprint to update.`}
    </CodeBlock>

    <h2>Client Flags</h2>
    <table>
      <thead>
        <tr><th>Flag</th><th>Default</th><th>Description</th></tr>
      </thead>
      <tbody>
        <tr><td><code>-addr</code></td><td><code>""</code></td><td>Remote daemon address (e.g., <code>myserver:9119</code>)</td></tr>
        <tr><td><code>-token</code></td><td><code>""</code></td><td>Bearer token for authentication (falls back to config)</td></tr>
        <tr><td><code>-tls</code></td><td><code>true</code></td><td>Use TLS for TCP connections</td></tr>
        <tr><td><code>-tls-skip-verify</code></td><td><code>false</code></td><td>Skip fingerprint verification</td></tr>
        <tr><td><code>-tls-reset-fingerprint</code></td><td><code>false</code></td><td>Update stored fingerprint for this server</td></tr>
      </tbody>
    </table>

    <h2>Authentication</h2>
    <p>
      When <code>auth_token</code> is set in the daemon config, all TCP connections must include the token via the
      <code>-token</code> flag or config file. The token is transmitted as a Bearer token in the HTTP Authorization
      header and compared using constant-time comparison.
    </p>
    <p>
      Unix socket connections are <strong>never authenticated</strong> — filesystem permissions are sufficient.
      The daemon logs a warning at startup if TCP is enabled without an auth token.
    </p>

    <h2>All Subcommands Support Remote</h2>
    <CodeBlock>
{`bewitch -addr myserver:9119 -token secret           # TUI
bewitch -addr myserver:9119 -token secret repl       # SQL REPL
bewitch -addr myserver:9119 -token secret compact    # trigger compaction
bewitch -addr myserver:9119 -token secret archive    # trigger archival
bewitch -addr myserver:9119 -token secret snapshot /tmp/remote.duckdb`}
    </CodeBlock>

    <h2>How It Works</h2>
    <p>
      The daemon runs separate server instances for the unix socket and TCP listener, sharing the same API.
      The unix socket has no auth (filesystem permissions suffice). The TCP listener applies bearer token
      middleware. Both are shut down gracefully on daemon exit.
    </p>
    <p>
      The client transparently adds the Authorization header to every request. TLS fingerprint pinning
      verifies the server certificate's fingerprint directly, bypassing CA chain validation — similar to
      SSH known_hosts.
    </p>
  </DocsLayout>
)

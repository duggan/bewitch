import type { FC } from 'hono/jsx'
import { DocsLayout } from '../../layouts/docs'
import { CodeBlock } from '../../components/terminal-block'

export const InstallationDocs: FC = () => (
  <DocsLayout title="Installation" active="/docs/installation">
    <p>
      Bewitch runs on Linux (amd64 and arm64). It uses procfs and sysfs for metric collection.
    </p>

    <h2>Supported Platforms</h2>
    <table>
      <thead>
        <tr><th>Platform</th><th>Install method</th></tr>
      </thead>
      <tbody>
        <tr><td>Debian 12+ / Ubuntu 22.04+</td><td>APT repository or <code>.deb</code> package</td></tr>
        <tr><td>Fedora, RHEL, Arch, Alpine, other Linux</td><td>Pre-built binary tarball</td></tr>
      </tbody>
    </table>
    <p>
      Architectures: <strong>amd64</strong> and <strong>arm64</strong> (Raspberry Pi 4/5, AWS Graviton, etc.)
    </p>

    <h2>Quick Install</h2>
    <p>The fastest way to install on any Linux distribution:</p>

    <CodeBlock title="one-line install">
{`curl -fsSL https://bewitch.dev/install.sh | sudo sh`}
    </CodeBlock>

    <p>
      On Debian/Ubuntu, this adds the APT repository, imports the signing key, and installs the package.
      Updates are handled through <code>apt upgrade</code>.
    </p>
    <p>
      On other distributions, it downloads a pre-built binary tarball, installs the binaries to
      <code>/usr/local/bin/</code>, creates a system user, and sets up the systemd service.
    </p>

    <h2>APT Repository (Debian/Ubuntu)</h2>
    <p>To add the repository manually:</p>

    <CodeBlock title="add repository">
{`# Import signing key
curl -fsSL https://bewitch.dev/gpg | sudo gpg --dearmor -o /usr/share/keyrings/bewitch.gpg

# Add repository
echo "deb [signed-by=/usr/share/keyrings/bewitch.gpg] https://bewitch.dev/apt stable main" \\
  | sudo tee /etc/apt/sources.list.d/bewitch.list

# Install
sudo apt update && sudo apt install bewitch`}
    </CodeBlock>

    <h2>Binary Tarball (Any Linux)</h2>
    <p>Download and install pre-built binaries directly:</p>

    <CodeBlock title="download & install">
{`# Download (replace ARCH with amd64 or arm64)
curl -LO https://bewitch.dev/releases/bewitch-0.1.0-linux-\${ARCH}.tar.gz
tar xzf bewitch-0.1.0-linux-*.tar.gz

# Install binaries
sudo install -m 755 bewitch-0.1.0-linux-*/bewitchd /usr/local/bin/
sudo install -m 755 bewitch-0.1.0-linux-*/bewitch /usr/local/bin/

# Set up system user and data directory
sudo useradd -r -s /usr/sbin/nologin bewitch
sudo mkdir -p /var/lib/bewitch
sudo chown bewitch:bewitch /var/lib/bewitch
sudo cp bewitch-0.1.0-linux-*/bewitch.example.toml /etc/bewitch.toml

# Install systemd service
sudo cp bewitch-0.1.0-linux-*/bewitchd.service /etc/systemd/system/
sudo systemctl daemon-reload`}
    </CodeBlock>

    <h2>Direct .deb Download</h2>
    <p>Download and install the <code>.deb</code> package directly (Debian/Ubuntu only):</p>

    <CodeBlock title="download & install">
{`# Replace ARCH with amd64 or arm64
curl -LO https://bewitch.dev/apt/pool/main/b/bewitch/bewitch_0.1.0-1_\${ARCH}.deb
sudo dpkg -i bewitch_0.1.0-1_*.deb`}
    </CodeBlock>

    <p>The <code>.deb</code> package automatically:</p>
    <ul>
      <li>Creates the <code>bewitch</code> system user and group</li>
      <li>Installs binaries to <code>/usr/local/bin/</code></li>
      <li>Sets up <code>/var/lib/bewitch/</code> with correct ownership</li>
      <li>Installs the systemd service (<code>bewitchd.service</code>)</li>
      <li>Copies example config to <code>/etc/bewitch.toml</code></li>
      <li>Configures disk and SMART access permissions</li>
    </ul>

    <h2>Build from Source</h2>
    <p>
      Requires <strong>Go 1.21+</strong> and a C compiler (for CGO/DuckDB).
    </p>

    <CodeBlock title="build & install">
{`git clone https://github.com/duggan/bewitch
cd bewitch
make build
sudo make install
sudo useradd -r -s /usr/sbin/nologin bewitch
sudo mkdir -p /var/lib/bewitch
sudo chown bewitch:bewitch /var/lib/bewitch
sudo cp bewitch.example.toml /etc/bewitch.toml
sudo cp debian/bewitchd.service /etc/systemd/system/
sudo systemctl daemon-reload`}
    </CodeBlock>

    <h2>systemd Service</h2>
    <p>Start and enable the daemon:</p>

    <CodeBlock title="start the daemon">
{`sudo systemctl enable --now bewitchd`}
    </CodeBlock>

    <p>
      The service uses <code>RuntimeDirectory=bewitch</code> which creates <code>/run/bewitch/</code> automatically.
      The default socket path is <code>/run/bewitch/bewitch.sock</code> (world-accessible, 0666).
    </p>

    <h3>Service management</h3>
    <CodeBlock>
{`sudo systemctl status bewitchd     # check status
sudo systemctl restart bewitchd    # restart after config changes
sudo journalctl -u bewitchd -f     # follow logs`}
    </CodeBlock>

    <h2>File Locations</h2>
    <table>
      <thead>
        <tr><th>Path</th><th>Purpose</th></tr>
      </thead>
      <tbody>
        <tr><td><code>/usr/local/bin/bewitchd</code></td><td>Daemon binary</td></tr>
        <tr><td><code>/usr/local/bin/bewitch</code></td><td>TUI + CLI binary</td></tr>
        <tr><td><code>/etc/bewitch.toml</code></td><td>Configuration file</td></tr>
        <tr><td><code>/var/lib/bewitch/</code></td><td>Data directory (DuckDB, TLS certs, Parquet archives)</td></tr>
        <tr><td><code>/run/bewitch/bewitch.sock</code></td><td>Unix socket (created by systemd)</td></tr>
        <tr><td><code>~/.config/bewitch/known_hosts</code></td><td>TLS fingerprints for remote connections</td></tr>
        <tr><td><code>~/.bewitch_sql_history</code></td><td>REPL command history</td></tr>
      </tbody>
    </table>

    <h2>Verify Installation</h2>
    <CodeBlock>
{`# Check the daemon is running
sudo systemctl status bewitchd

# Launch the TUI
bewitch

# Or query the API directly
curl --unix-socket /run/bewitch/bewitch.sock http://localhost/api/status`}
    </CodeBlock>

    <h2>Uninstall</h2>
    <h3>Debian/Ubuntu (APT)</h3>
    <CodeBlock>
{`sudo apt remove bewitch
sudo rm /etc/apt/sources.list.d/bewitch.list
sudo rm /usr/share/keyrings/bewitch.gpg`}
    </CodeBlock>

    <h3>Binary install</h3>
    <CodeBlock>
{`sudo systemctl disable --now bewitchd
sudo rm /usr/local/bin/bewitchd /usr/local/bin/bewitch
sudo rm /etc/systemd/system/bewitchd.service
sudo systemctl daemon-reload`}
    </CodeBlock>
  </DocsLayout>
)

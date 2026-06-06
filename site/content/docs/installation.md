+++
title = "Installation"
description = "One-line installer, the Debian APT repo, or prebuilt binaries — plus the systemd unit."
weight = 10
+++

Bewitch runs on Linux (amd64 and arm64). It uses procfs and sysfs for metric collection.

## Supported Platforms

| Platform | Install method |
| --- | --- |
| Debian 12+ / Ubuntu 22.04+ | APT repository or `.deb` package |
| Fedora, RHEL, Arch, Alpine, other Linux | Pre-built binary tarball |

Architectures: **amd64** and **arm64** (Raspberry Pi 4/5, AWS Graviton, etc.)

## Quick Install

The fastest way to install on any Linux distribution:

```bash
curl -fsSL https://bewitch.dev/install.sh | sudo sh
```

On Debian/Ubuntu, this adds the APT repository, imports the signing key, and installs the package.
Updates are handled through `apt upgrade`.

On other distributions, it downloads a pre-built binary tarball, installs the binaries to
`/usr/local/bin/`, creates a system user, and sets up the systemd service.

## APT Repository (Debian/Ubuntu)

To add the repository manually:

```bash
# Import signing key
curl -fsSL https://bewitch.dev/gpg | sudo gpg --dearmor -o /usr/share/keyrings/bewitch.gpg

# Add repository
echo "deb [signed-by=/usr/share/keyrings/bewitch.gpg] https://bewitch.dev/apt stable main" \
  | sudo tee /etc/apt/sources.list.d/bewitch.list

# Install
sudo apt update && sudo apt install bewitch
```

### Dev channel

To track the latest development builds (published on every push to `main`):

```bash
# Replace "stable" with "dev" in the repository line
echo "deb [signed-by=/usr/share/keyrings/bewitch.gpg] https://bewitch.dev/apt dev main" \
  | sudo tee /etc/apt/sources.list.d/bewitch.list

sudo apt update && sudo apt install bewitch
```

Dev builds use versions like `0.1.3~dev.20260314.abcdef1`. The `~` prefix means
APT treats them as older than the corresponding release, so switching back to `stable` and
running `apt upgrade` will move you to the latest release automatically.

Or use the quick install script with the dev channel:

```bash
curl -fsSL https://bewitch.dev/install.sh | BEWITCH_CHANNEL=dev sudo -E sh
```

## Binary Tarball (Any Linux)

Download and install pre-built binaries directly:

```bash
# Download (replace ARCH with amd64 or arm64)
curl -LO https://bewitch.dev/releases/bewitch-0.6.0-linux-${ARCH}.tar.gz
tar xzf bewitch-0.6.0-linux-*.tar.gz

# Install binaries
sudo install -m 755 bewitch-0.6.0-linux-*/bewitchd /usr/local/bin/
sudo install -m 755 bewitch-0.6.0-linux-*/bewitch /usr/local/bin/

# Set up system user and data directory
sudo useradd -r -s /usr/sbin/nologin bewitch
sudo mkdir -p /var/lib/bewitch
sudo chown bewitch:bewitch /var/lib/bewitch
sudo cp bewitch-0.6.0-linux-*/bewitch.example.toml /etc/bewitch.toml

# Install systemd service
sudo cp bewitch-0.6.0-linux-*/bewitchd.service /etc/systemd/system/
sudo systemctl daemon-reload
```

## Direct .deb Download

Download and install the `.deb` package directly (Debian/Ubuntu only):

```bash
# Replace ARCH with amd64 or arm64
curl -LO https://bewitch.dev/apt/pool/main/b/bewitch/bewitch_0.6.0-1_${ARCH}.deb
sudo dpkg -i bewitch_0.6.0-1_*.deb
```

The `.deb` package automatically:

- Creates the `bewitch` system user and group
- Installs binaries to `/usr/local/bin/`
- Sets up `/var/lib/bewitch/` with correct ownership
- Installs, enables, and starts the systemd service (`bewitchd.service`)
- Copies example config to `/etc/bewitch.toml`
- Configures disk and SMART access permissions

## Build from Source

Requires **Go 1.21+** and a C compiler (for CGO/DuckDB).

```bash
git clone https://github.com/duggan/bewitch
cd bewitch
make build
sudo make install
sudo useradd -r -s /usr/sbin/nologin bewitch
sudo mkdir -p /var/lib/bewitch
sudo chown bewitch:bewitch /var/lib/bewitch
sudo cp bewitch.example.toml /etc/bewitch.toml
sudo cp debian/bewitchd.service /etc/systemd/system/
sudo systemctl daemon-reload
```

## systemd Service

The install script and `.deb` package automatically enable and start the daemon.
For build-from-source installs, start it manually:

```bash
sudo systemctl enable --now bewitchd
```

The service uses `RuntimeDirectory=bewitch` which creates `/run/bewitch/` automatically.
The default socket path is `/run/bewitch/bewitch.sock` (world-accessible, 0666).

### Service management

```bash
sudo systemctl status bewitchd     # check status
sudo systemctl restart bewitchd    # restart after config changes
sudo journalctl -u bewitchd -f     # follow logs
```

## Optional Dependencies

The installer detects hardware and offers to install optional monitoring tools.
These can also be installed manually:

- `smartmontools` — enhanced SMART disk health via `smartctl` (installed automatically on Debian/Ubuntu)
- `intel-gpu-tools` — Intel iGPU monitoring via `intel_gpu_top`
- NVIDIA driver — provides `nvidia-smi` for NVIDIA GPU monitoring

Use `BEWITCH_NONINTERACTIVE=1` to auto-install all detected optional dependencies without prompting.

## File Locations

| Path | Purpose |
| --- | --- |
| `/usr/local/bin/bewitchd` | Daemon binary |
| `/usr/local/bin/bewitch` | TUI + CLI binary |
| `/etc/bewitch.toml` | Configuration file |
| `/var/lib/bewitch/` | Data directory (DuckDB, TLS certs, Parquet archives) |
| `/run/bewitch/bewitch.sock` | Unix socket (created by systemd) |
| `~/.config/bewitch/known_hosts` | TLS fingerprints for remote connections |
| `~/.bewitch_sql_history` | REPL command history |

## Verify Installation

```bash
# Check the daemon is running
sudo systemctl status bewitchd

# Launch the TUI
bewitch

# Or query the API directly
curl --unix-socket /run/bewitch/bewitch.sock http://localhost/api/status
```

## Uninstall

Run the uninstall script to remove bewitch, the systemd service, config, data, and system user:

```bash
curl -fsSL https://bewitch.dev/uninstall.sh | sudo sh
```

On Debian/Ubuntu, this removes the APT package and repository.
On other systems, it removes the files installed by the tarball.

To keep the database and archives in `/var/lib/bewitch/`:

```bash
curl -fsSL https://bewitch.dev/uninstall.sh | sudo KEEP_DATA=1 sh
```

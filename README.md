# synco

A file synchronization CLI tool. Supports one-way real-time sync between local directories, remote servers (TCP), Google Drive, and Dropbox.

## Supported Scenarios

| src | dst | Description |
|-----|-----|-------------|
| Local | Local | Sync between directories on the same machine |
| Local | Remote TCP | Real-time transfer to another machine over LAN/WAN |
| Remote TCP | Local | Receive files from a remote machine to local |
| Local | Google Drive | Upload a local directory to a GDrive folder |
| Google Drive | Local | Download a GDrive folder to a local directory |
| Local | Dropbox | Upload a local directory to a Dropbox folder |
| Dropbox | Local | Download a Dropbox folder to a local directory |

## Installation

```bash
git clone https://github.com/your-username/synco
cd synco
go build -o synco .
```

Move the binary to a directory in your PATH.

```bash
# Linux/macOS
mv synco /usr/local/bin/

# Windows: move synco.exe to a directory included in PATH
```

## Getting Started

### 1. Register for auto-start on login (recommended)

```bash
synco install
```

The daemon will start automatically after reboots. Registers as a systemd user service on Linux, and as a Task Scheduler entry on Windows.

### 2. Authenticate with cloud providers (if using Google Drive or Dropbox)

```bash
# Google Drive
synco auth gdrive

# Dropbox
synco auth dropbox
```

### 3. Add a sync job

```bash
# Local → Local
synco job add /path/to/src /path/to/dst

# Local → Google Drive
synco job add /path/to/src gdrive:/MyFolder/SubFolder

# Local → Dropbox
synco job add /path/to/src dropbox:/MyFolder/SubFolder

# Local → Remote TCP (the remote machine must have synco installed and the daemon running)
synco job add /path/to/src 192.168.1.10:9000/path/to/dst

# Remote TCP → Local (the remote daemon pushes files to the local machine)
synco job add 192.168.1.10:9000/path/to/src /path/to/dst
```

## Commands

### Job Management

```bash
synco job add [src] [dst]              # Register a job
synco job add --once [src] [dst]       # Sync once and exit
synco job add --foreground [src] [dst] # Run daemon in the foreground

synco job list                         # List all registered jobs
synco job remove [id]                  # Remove a job
synco job pause [id]                   # Pause a job
synco job resume [id]                  # Resume a job
```

### Daemon

```bash
synco install                          # Register auto-start + start immediately
synco uninstall                        # Unregister auto-start
synco stop                             # Stop the daemon
synco status                           # Show daemon status and job list
synco daemon start                     # Start the daemon manually
```

### History

```bash
synco history                          # Recent sync history (default: 20 entries)
synco history --n 50                   # Recent 50 entries
synco history --job-id 3               # History for a specific job
```

### Authentication

```bash
synco auth gdrive                      # Authenticate with Google Drive (OAuth)
synco auth dropbox                     # Authenticate with Dropbox (OAuth)
```

## Endpoint Format

| Format | Example |
|--------|---------|
| Local path | `/home/user/docs`, `C:\Users\user\docs` |
| Remote TCP | `192.168.1.10:9000/path/to/dir` |
| Google Drive | `gdrive:/FolderName/SubFolder` |
| Dropbox | `dropbox:/FolderName/SubFolder` |

## Configuration

Settings can be changed in `~/.synco/config.yaml`. If the file does not exist, default values are used.

```yaml
daemon_port: 9001              # Daemon HTTP API port
buffer_size: 100               # Event buffer size
conflict_strategy: newer_wins  # Conflict resolution strategy: newer_wins | src_wins | dst_wins
ignore_list:                   # Patterns to exclude from sync
  - .git
  - .DS_Store
  - "*.tmp"
  - "*.swp"
```

## Architecture

synco consists of a background daemon process and a CLI client.

```
CLI (synco job add ...)
    │
    │ HTTPS (127.0.0.1, Bearer token)
    ▼
Daemon (background process)
    │
    ├── LocalSource (fsnotify)
    ├── GDriveSource (Changes API, polling)
    └── DropboxSource (ListFolder/Longpoll)
         │
         ▼
    Pipeline (Debounce → Filter → ChecksumFilter)
         │
         ▼
    Syncer (LocalSyncer / TCPSyncer / GDriveUploader / DropboxUploader / ...)
```

### Remote TCP Sync

Remote TCP operates in two directions.

**Local → Remote (push)**: The local daemon detects file events and sends them directly to the remote machine's TCP server.

**Remote → Local (delegation)**: The local daemon sends a delegation request to the remote daemon, asking it to push files to a specific local address. The remote daemon then detects file events and forwards them to the local machine's receiving port.

### Security

The daemon HTTP API has the following security measures applied.

- **HTTPS**: Self-signed certificate (auto-generated at `~/.synco/daemon.crt` on first run)
- **Bearer token**: All API requests require token authentication (`~/.synco/token`)
- **Localhost binding**: Bound to `127.0.0.1` only, preventing exposure to external networks
- **Timestamp validation**: Replay attack prevention for daemon-to-daemon communication (5-minute validity window)

### Conflict Resolution

For bidirectional sync over TCP, conflicts are detected using Vector Clocks. When a conflict occurs, it is resolved according to the configured strategy.

| Strategy | Behavior |
|----------|----------|
| `newer_wins` | Keep the file with the more recent modification time (default) |
| `src_wins` | Always overwrite with the src file |
| `dst_wins` | Always keep the dst file |

## Data Storage

| Item | Path |
|------|------|
| Config file | `~/.synco/config.yaml` |
| Database | `~/.synco/synco.db` |
| TLS certificate | `~/.synco/daemon.crt`, `~/.synco/daemon.key` |
| API token | `~/.synco/token` |
| GDrive page token | `~/.synco/gdrive_pagetoken_{jobID}` |
| Dropbox cursor | `~/.synco/dropbox_cursor_{jobID}` |
| GDrive OAuth token | `~/.synco/gdrive_token.json` |
| Dropbox OAuth token | `~/.synco/dropbox_token.json` |

## Known Limitations

- Bidirectional cloud sync is not supported (GDrive and Dropbox are one-way only). Registering two jobs in opposite directions will cause an infinite sync loop, as the cloud sources cannot distinguish between changes made by synco and external changes.
- Mutual TLS between daemons is not implemented — assumes a trusted internal network environment.
- No test coverage.

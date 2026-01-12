# nazim (ناظم)

[![CI](https://github.com/calilkhalil/nazim/workflows/CI/badge.svg)](https://github.com/calilkhalil/nazim/actions)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/go-1.23+-blue.svg)](https://golang.org)
[![Go Report Card](https://goreportcard.com/badge/github.com/calilkhalil/nazim)](https://goreportcard.com/report/github.com/calilkhalil/nazim)

> Multi-OS service manager. Create and manage services (scripts/commands) that can be executed on system startup or at regular intervals.

*nazim* (ناظم) means "organizer" or "manager" in Arabic — what this tool does for your system services.

## Why?

Managing system services across different operating systems is boring. Each platform has its own mechanism:
- **Windows**: Task Scheduler
- **Linux**: systemd
- **macOS**: launchd

**nazim simplifies this.** It provides a unified CLI to manage services across all platforms. Write your service once, and nazim handles the platform-specific installation automatically.

```mermaid
flowchart LR
    A[Script<br/>or Command] -->|nazim add| B[nazim]
    B -->|detects platform| C{Platform}
    C -->|Linux| D[systemd]
    C -->|macOS| E[launchd]
    C -->|Windows| F[Task Scheduler]
    D --> G[Service Installed]
    E --> G
    F --> G
```

## Features

- **Multi-Platform**: Works on Windows, Linux, and macOS
- **Startup Services**: Run commands automatically on system boot
- **Scheduled Execution**: Execute services at regular intervals (minutes, hours, days)
- **Interactive Editor**: Use `--command write` to open your default editor and create scripts
- **Service Management**: Edit, enable, disable, run, and view service status
- **Automatic Logging**: All service output is automatically logged to `~/.config/nazim/logs/` (or `%APPDATA%\nazim\logs\` on Windows)
- **Simple CLI**: Easy-to-use command-line interface
- **XDG Compliant**: Follows XDG Base Directory Specification for config files
- **Minimal Dependencies**: Uses Go standard library, YAML parser, and Windows API for UAC elevation

## Quick Start

```sh
# Clone and build
git clone https://github.com/calilkhalil/nazim
cd nazim
make build

# Or install directly
make install-user  # Installs to ~/.local/bin
```

Add your first service:

```sh
# Simple one-liner: clean temp files every hour
nazim add --name cleanup --command "rm -rf /tmp/old_files" --interval 1h

# Or use a script file
nazim add --name backup --command backup.sh --interval 1h

# Or use interactive mode to write a script
nazim add --name myscript --command write --interval 1h
```

## Installation

### From Source

```sh
git clone https://github.com/calilkhalil/nazim
cd nazim
make build
sudo make install       # System-wide installation
make install-user       # User installation (~/.local/bin)
```

### Using Go Install

```sh
go install github.com/calilkhalil/nazim/cmd/nazim@latest
```

## Usage

nazim stores service configuration in `~/.config/nazim/services.yaml` (or `%APPDATA%\nazim\services.yaml` on Windows).

### Basic Commands

```sh
# Add a service with a simple command (oneliner)
nazim add --name "cleanup" --command "rm -rf /tmp/old_files" --interval "1h"

# Add a service with a script file
nazim add --name "backup" --command "backup.sh" --interval "1h"

# Add a service that runs on startup
nazim add --name "init" --command "init.sh" --on-startup

# Interactive mode: open editor to write script
nazim add --name "myscript" --command write --interval "30m"

# List all services
nazim list

# Remove a service
nazim remove backup

# Run a service immediately (independent of schedule)
nazim run backup

# Show service status
nazim status backup
# Or use the alias
nazim info backup
```

### Examples

#### Simple One-liner Commands

```sh
# Clean up temporary files every hour
nazim add --name "cleanup-tmp" --command "rm -rf /tmp/old_files" --interval "1h"

# Run a backup command every day
nazim add --name "daily-backup" --command "tar -czf /backup/data-$(date +%Y%m%d).tar.gz /data" --interval "24h"

# Send a notification on startup
nazim add --name "startup-notify" --command "curl -X POST https://api.example.com/notify" --on-startup

# Windows: Clean temp files
nazim add --name "clean-temp" --command "del /Q C:\temp\*.tmp" --interval "30m"
```

#### Script Files

```sh
# Add a service that runs a script every 5 minutes
nazim add --name "monitor" --command "monitor.sh" --interval "5m"

# Add a service that runs on system startup
nazim add --name "init-script" --command "init.sh" --on-startup
```

#### Interactive Mode (Write Script in Editor)

```sh
# Open your default editor to write a script
# The script will be saved in ~/.config/nazim/scripts/
nazim add --name "custom-task" --command write --interval "1h"
```

#### Commands with Arguments

```sh
# Add a service with arguments and working directory
nazim add --name "processor" \
  --command "python" \
  --args "process.py --verbose" \
  --workdir "/opt/app" \
  --interval "30m"

# One-liner with arguments (use quotes)
nazim add --name "log-analyzer" \
  --command "python" \
  --args "analyze.py --input /var/log/app.log --output /tmp/report.json" \
  --interval "15m"
```

#### Management

```sh
# List all services with status
nazim list

# Show detailed information about a service
nazim status backup
# Or use the alias
nazim info backup

# Edit an existing service
nazim edit backup --interval 2h          # Change to interval-only (disables startup)
nazim edit backup --on-startup           # Change to startup-only (removes interval)
nazim edit backup --command newscript.sh # Update command only
nazim edit --name backup --interval 2h   # Using --name flag

# Enable/disable services
nazim enable backup
nazim disable backup

# Remove a service (also uninstalls from system)
nazim remove monitor

# Run a service immediately (independent of schedule)
nazim run backup

# Show version information
nazim version
```

## Commands

```
nazim add <options>     add a new service
nazim list              list all services
nazim status <name>    show detailed service information (alias: info)
nazim edit <name>       update an existing service
nazim remove <name>     remove a service (from system and config)
nazim enable <name>     enable a service
nazim disable <name>    disable a service
nazim run <name>        execute a service immediately (independent of schedule)
nazim version           show version information
```

### Add Command Options

- `-n, --name <name>`        service name (required)
- `-c, --command <cmd>`      command or script to execute (required)
                             - Simple command: `"rm -rf /tmp/old_files"`
                             - Script file: `backup.sh`
                             - Interactive: `write` or `edit` (opens editor)
- `-a, --args <args>`        arguments for the command (space-separated)
- `-w, --workdir <dir>`      working directory
- `--on-startup`             run on system startup (mutually exclusive with interval)
- `-i, --interval <dur>`     execution interval (e.g., 5m, 1h, 30s) (mutually exclusive with startup)

**Note:** `--on-startup` and `--interval` are mutually exclusive. A service can run either on startup OR at intervals, not both.

### Edit Command Options

- `-n, --name <name>`        service name (can be provided as flag or positional argument)
- `-c, --command <cmd>`      update the command
- `-a, --args <args>`        update arguments
- `-w, --workdir <dir>`      update working directory
- `--on-startup`             enable startup mode (disables interval)
- `-i, --interval <dur>`     enable interval mode (disables startup)

**Behavior:**
- If `--on-startup` is provided, the service will run only on startup (interval is cleared)
- If `--interval` is provided, the service will run at intervals (startup is disabled)
- If neither is provided, the current startup/interval setting is preserved
- Other fields (command, args, workdir) are only updated if explicitly provided
- Service name can be specified with `--name` flag or as a positional argument: `nazim edit backup` or `nazim edit --name backup`

### Global Options

- `-v, --verbose`            enable verbose output
- `-h, --help`               show help
- `--version`                show version information

## Interval Format

Intervals can be specified using suffixes:
- `30s` - 30 seconds
- `5m` - 5 minutes
- `1h` - 1 hour
- `24h` - 24 hours
- `7d` - 7 days

## Configuration

Services are stored in YAML format at:
- **Linux/macOS**: `~/.config/nazim/services.yaml` (or `$XDG_CONFIG_HOME/nazim/services.yaml`)
- **Windows**: `%APPDATA%\nazim\services.yaml`

Scripts created via `--command write` are stored in:
- **Linux/macOS**: `~/.config/nazim/scripts/`
- **Windows**: `%APPDATA%\nazim\scripts\`

Service logs are automatically stored in:
- **Linux/macOS**: `~/.config/nazim/logs/`
- **Windows**: `%APPDATA%\nazim\logs\`

Example configuration:

```yaml
- name: backup
  command: backup.sh
  interval: 1h
  enabled: true
  platform: linux

- name: init-script
  command: init.sh
  on_startup: true
  enabled: true
  platform: linux
```

**Note:** `on_startup` and `interval` are mutually exclusive. A service can have either `on_startup: true` OR an `interval`, but not both.

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `NAZIM_VERBOSE` | Enable verbose output | (unset) |
| `EDITOR` | Default editor for interactive mode | Platform-specific (vim, nano, notepad, etc.) |
| `VISUAL` | Alternative editor variable | Same as EDITOR |
| `XDG_CONFIG_HOME` | Config directory | ~/.config (Linux/macOS), %APPDATA% (Windows) |

### Interactive Editor Mode

When using `--command write` or `--command edit`, nazim will:
1. Create a script template with:
   - Service name clearly marked
   - Section marked "YOUR CODE HERE" where you write your logic
   - Examples and comments to guide you
   - Proper shebang/header for your platform
2. Open your default editor (from `$EDITOR` or `$VISUAL` env vars)
3. Save the script in `~/.config/nazim/scripts/` (or equivalent)
4. Use the appropriate extension (`.sh` on Linux/macOS, `.bat` on Windows)
5. Make the script executable automatically

**Template Structure:**
- Header with service name, creation date, and location
- Clear section marked "YOUR CODE HERE" for your code
- Examples of common commands
- Proper exit codes

The editor priority:
- `$EDITOR` environment variable
- `$VISUAL` environment variable
- Platform defaults: `vim`, `nano`, `code` (VS Code), or `notepad` (Windows)

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Error |

## Platform Support

### Windows
- Uses **Task Scheduler** (`schtasks`) for service management
- Supports startup and scheduled execution
- Services are prefixed with `Nazim_` in Task Scheduler
- Automatic UAC elevation when needed for service installation/management

### Linux
- Uses **systemd** (user services) exclusively
- Requires systemd to be available (most modern Linux distributions)
- Services are created in `~/.config/systemd/user/`
- Uses systemd timers for scheduled execution

### macOS
- Uses **launchd** for service management
- Services are created in `~/Library/LaunchAgents/`
- Supports startup and interval-based execution

## How It Works

1. **Add Service**: nazim validates the service configuration and saves it to YAML
2. **Install**: nazim creates the appropriate platform-specific service:
   - Windows: Task Scheduler entry (with automatic log redirection)
   - Linux: systemd service/timer (with automatic log redirection)
   - macOS: launchd plist file (with automatic log redirection)
3. **Logging**: All service output (stdout and stderr) is automatically redirected to log files in `~/.config/nazim/logs/` (or `%APPDATA%\nazim\logs\` on Windows)
4. **Manage**: You can list, run, edit, enable, disable, or remove services through the CLI
5. **Remove**: nazim uninstalls the service from the system, removes it from config, and deletes associated script files (if created via `write` command)

## Requirements

- Go 1.24.0 or higher (for building from source)
- Administrator/root permissions (for installing system services)
- Platform-specific tools:
  - Windows: `schtasks`
  - Linux: `systemctl`
  - macOS: `launchctl`

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License - see [LICENSE](LICENSE) file for details.

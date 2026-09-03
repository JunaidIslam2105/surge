# Legacy usage guide

The task-oriented documentation has moved to the [documentation index](README.md).
For the current command list and flags, see the [CLI reference](reference/cli.md).

# Usage Guide

Surge has both a robust CLI and a fast Interactive TUI. For configuration options, see [SETTINGS.md](SETTINGS.md).

## TUI Usage

In the interactive TUI (launched simply via `surge`), you can manage your downloads through keyboard shortcuts. Press `h` to open or close the keyboard-shortcuts overlay. Press `?` to report a bug.

### Adding Downloads from Clipboard

While in the TUI Dashboard, you can rapidly add downloads using your clipboard:
- Press `a` to manually type or paste a URL.
- Press `Shift+A` to directly attempt to parse a copied browser **cURL** command (from "Copy as cURL"). Surge will extract the URL and all headers (like cookies and user-agents).

## CLI Commands

## Command Table

| Command                     | What it does                                                                           | Key flags                                                                                           | Notes                                                                   |
| :-------------------------- | :------------------------------------------------------------------------------------- | :-------------------------------------------------------------------------------------------------- | :---------------------------------------------------------------------- |
| `surge [url]...`            | Launches local TUI. Queues optional URLs.                                              | `--batch, -b`<br>`--port, -p`<br>`--output, -o`<br>`--no-resume`<br>`--exit-when-done`<br>`--no-server` | `-o` defaults to CWD. If `--host` is set, this becomes remote TUI mode. `--no-server` disables the embedded HTTP API for that session. |
| `surge server [url]...`     | Launches headless server. Queues optional URLs.                                        | `--batch, -b`<br>`--port, -p`<br>`--output, -o`<br>`--exit-when-done`<br>`--no-resume`<br>`--token` | `-o` defaults to CWD. Primary headless mode command.                    |
| `surge connect [host:port]` | Launches TUI connected to a server. Auto-detects local server when no target is given. | `--insecure-http`                                                                                   | Convenience alias for remote TUI usage.                                 |
| `surge add <url>...`        | Queues downloads via CLI/API.                                                          | `--batch, -b`<br>`--output, -o`                                                                     | `-o` defaults to CWD. Alias: `get`.                                     |
| `surge ls [id]`             | Lists downloads, or shows one download detail.                                         | `--json`<br>`--watch`                                                                               | Alias: `l`.                                                             |
| `surge limit <id> <speed>`  | Sets per-download, global, or default speed limits.                                    | `--global`<br>`--default`                                                                           | Use `unlimited`/`0` to disable, or `inherit` for per-download default.   |
| `surge pause <id>`          | Pauses a download by ID/prefix.                                                        | `--all`                                                                                             |                                                                         |
| `surge resume <id>`         | Resumes a paused download by ID/prefix.                                                | `--all`                                                                                             |                                                                         |
| `surge refresh <id> <url>`  | Updates the source URL of a paused or errored download.                                | None                                                                                                | Reconnects using the new link.                                          |
| `surge rm <id>`             | Removes a download by ID/prefix.                                                       | `--clean`, `--purge`                                                                                | Alias: `kill`.                                                          |
| `surge config [path] [val]` | Get, set, or reset Surge configuration options via the CLI.                            | None                                                                                                | See [SETTINGS.md](SETTINGS.md) for available settings. Run without args to list all. |
| `surge token`               | Prints current API auth token. (Also visible in TUI > Settings > Extension)            | None                                                                                                | Useful for remote clients.                                              |
| `surge service <cmd>`       | Manages Surge as a system service (daemon).                                            | `install`, `uninstall`, `start`, `stop`, `status`, `token`                                      | Cross-platform (Linux/Windows/macOS). See [Service Management](#service-management). |
| `surge bug-report`          | Opens a pre-filled GitHub bug report. Prompts for target (Core/Extension) and optional system/log details. | None                                                                                                | Prints a manual URL fallback if browser open fails.                     |

## Service Management

The `service` command allows you to manage Surge as a background daemon that starts automatically on boot.

- `surge service install`: Registers Surge as a system service.
- `surge service uninstall`: Removes the system service.
- `surge service start`: Starts the background service.
- `surge service stop`: Stops the background service.
- `surge service status`: Checks if the service is installed and running.
- `surge service token`: Prints the auth token used by the system service daemon.

**Note**: On most systems, these commands require administrative privileges (e.g., `sudo surge service install`).

## Server Subcommands (Compatibility)

| Command                       | What it does                                           |
| :---------------------------- | :----------------------------------------------------- |
| `surge server start [url]...` | Legacy equivalent of `surge server [url]...`.          |
| `surge server stop`           | Stops a running server process by PID file.            |
| `surge server status`         | Prints running/not-running status from PID/port state. |

## Global Flags

These are persistent flags and can be used with all commands.

| Flag                 | Description                            |
| :------------------- | :------------------------------------- |
| `--host <host:port>` | Target server for TUI and CLI actions. |
| `--token <token>`    | Bearer token used for API requests.    |

## Environment Variables

| Variable      | Description                                   |
| :------------ | :-------------------------------------------- |
| `SURGE_HOST`  | Default host when `--host` is not provided.   |
| `SURGE_TOKEN` | Default token when `--token` is not provided. |

## Fonts

Surge bundles a Nerd Font, but terminal fonts are controlled by your terminal
emulator. Install the bundled font and set your terminal to
`JetBrainsMono Nerd Font Mono`.

See [FONTS.md](FONTS.md) for install steps and licensing details.

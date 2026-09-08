# Get started with Surge

This guide gets a download running, then helps you choose whether the
interactive TUI, a headless server, or a system service is the right fit.

## Install Surge

Use the package manager or release artifact that suits your platform:

| Platform | Command or source |
| --- | --- |
| Windows | `winget install surge-downloader.surge`, `scoop install surge`, or `choco install surge` |
| macOS / Linux | `brew install SurgeDM/tap/surge` |
| Arch Linux | `yay -S surge` |
| Nix / NixOS | `nix run github:SurgeDM/Surge` |
| Linux / macOS (no package manager) | `curl -sSL https://raw.githubusercontent.com/SurgeDM/Surge/main/scripts/install.sh \| sh` |
| Any supported platform | [Download a release](https://github.com/SurgeDM/Surge/releases/latest) |

The install script detects your OS and architecture (including Linux ARM64,
e.g. Alpine/postmarketOS), downloads the matching release asset, verifies its
checksum, installs the binary to `~/.local/bin` (override with
`SURGE_INSTALL_DIR`), and sets up shell completion for zsh/bash/fish.

Run `surge --version` after installing to confirm that your shell can find the
binary.

## Make your first download

Pass a URL to `surge` to open the TUI with that download queued:

```bash
surge https://example.com/archive.zip
```

By default, the file is saved in the directory from which you ran the command.
Use `--output` to choose a destination:

```bash
surge https://example.com/archive.zip --output ~/Downloads
```

On Windows, use a PowerShell path such as `--output "$HOME\\Downloads"`.

## Choose a mode

Use the **TUI** when you want to watch and manage downloads in a terminal:

```bash
surge
```

Use the **headless server** for a machine without an interactive terminal, or
when commands and the browser extension should control a single background
download manager:

```bash
surge server
```

Use a **system service** when the server should start with the machine:

```bash
surge service install
surge service start
```

See [TUI, server, and remote modes](concepts/modes.md) for the differences and
[Run Surge as a service](guides/run-as-a-service.md) before installing a
service.

## Next steps

- [Add, inspect, pause, resume, and remove downloads](guides/download-files.md)
- [Learn the TUI controls](guides/use-the-tui.md)
- [Read the CLI reference](reference/cli.md)

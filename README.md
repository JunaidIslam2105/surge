<div align="center">

# Surge

**Blazing fast TUI download manager built in Go for power users**

[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/SurgeDM/Surge)
[![Release](https://img.shields.io/github/v/release/SurgeDM/Surge?style=flat-square&color=blue)](https://github.com/SurgeDM/Surge/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/SurgeDM/Surge?style=flat-square&color=cyan)](go.mod)
[![License](https://img.shields.io/badge/License-MIT-grey.svg?style=flat-square)](LICENSE)
[![BuyMeACoffee](https://raw.githubusercontent.com/pachadotdev/buymeacoffee-badges/main/bmc-violet.svg)](https://www.buymeacoffee.com/surge.downloader)
[![Stars](https://img.shields.io/github/stars/SurgeDM/Surge?style=social)](https://github.com/SurgeDM/Surge/stargazers)

[Documentation](docs/README.md) • [Installation](#installation) • [Usage](#usage) • [Themes](docs/guides/customize-surge.md) • [Fonts](docs/guides/customize-surge.md) • [Benchmarks](#benchmarks) • [Extension](#browser-extension) • [Settings](docs/SETTINGS.md) • [CLI Reference](docs/reference/cli.md)

</div>

---

## What is Surge?

Surge is designed for power users who prefer a keyboard-driven workflow. It features a beautiful **Terminal User Interface (TUI)**, as well as a background **Headless Server** and a **CLI tool** for automation.

![Surge Demo](assets/demo.gif)

---

## Why use Surge?

Most browsers open a single connection for a download. Surge opens multiple (up to 16 by default), splits the file, and downloads chunks in parallel. But we take it a step further:

- **Blazing Fast:** Designed to maximize your bandwidth utilization and download files as quickly as possible.
- **Multiple Mirrors:** Download from multiple sources simultaneously. Surge distributes workers across all available mirrors and automatically handles failover.
- **Sequential Download:** Option to download files in strict order (Streaming Mode). Ideal for media files that you want to preview while downloading.
- **Daemon Architecture:** Surge runs a single background "engine." You can open 10 different terminal tabs and queue downloads; they all funnel into one efficient manager.
- **Beautiful TUI:** Built with Bubble Tea & Lipgloss, featuring customizable palettes and full theme engine support.

For a deep dive into how we make downloads faster (like work stealing and slow worker handling), check out our **[Optimization Guide](docs/OPTIMIZATIONS.md)**.

---

## Support the Project

We are just two CS students building Surge in between classes and exams. We love working on this, but maintaining a project of this scale takes time and resources. That's where you come in!

If Surge saves you time, consider supporting the development! Donations go directly toward:

- **Publishing the Extension:** Paying the Chrome Web Store fee so you can finally install the extension officially (no more sideloading!).
- **Dev Tools:** Licenses for tools like **GoReleaser Pro** to help us automate our builds.
- **Debrid Integration:** Covering subscription costs so we can test and build native Debrid support.

[**☕ Buy us a coffee**](https://www.buymeacoffee.com/surge.downloader)

_Totally optional-your stars, issues, and contributions already mean the world to us! :)_

---

## Installation

Surge is available on multiple platforms. Choose the method that works best for you.

| Platform / Method                  | Command / Instructions                                                           | Notes                                        |
| :--------------------------------- | :------------------------------------------------------------------------------- | :------------------------------------------- |
| **Prebuilt Binary**          | [Download from Releases](https://github.com/SurgeDM/Surge/releases/latest) | Easiest method. Just download and run.       |
| **Linux AppImage**           | [Download from Releases](https://github.com/SurgeDM/Surge/releases/latest) | Run `chmod +x Surge_..._linux_x86_64.AppImage`, then `./Surge_..._linux_x86_64.AppImage`. Supports delta updates. |
| **Arch Linux (AUR)**         | `yay -S surge`                                                                 | Managed via AUR.                             |
| **macOS / Linux (Homebrew)** | `brew install SurgeDM/tap/surge`                                      | Recommended for Mac/Linux users.             |
| **Nix / NixOS**              | `nix run github:SurgeDM/Surge`                                        | Via Nix flake. NixOS config: `inputs.surge.packages.${pkgs.system}.default` |
| **Windows**         | `winget install surge-downloader.surge`<br />or<br />`scoop install surge`<br />or<br />`choco install surge` | Recommended for Windows users.               |
| **Dockerfile**               | [See instructions](#4-server-mode-with-docker-compose)                              | Run Surge in server mode with Docker Compose |
| **Go Install**               | `go install github.com/SurgeDM/Surge@latest`                          | Requires Go 1.25+                           |

---

## Usage

Surge has two main modes: **TUI (Interactive)** and **Server (Headless)**.

For a full reference, see the **[customization guide](docs/guides/customize-surge.md)**, **[Settings &amp; Configuration Guide](docs/SETTINGS.md)** and the **[CLI reference](docs/reference/cli.md)**.

### 1. Interactive TUI Mode

Just run `surge` to enter the dashboard. This is where you can visualize progress, manage the queue, and see speed graphs. If you encounter any issues, press `?` to open the bug reporting wizard. You can also press `Shift + A` or use your terminal's paste shortcut to automatically parse a copied browser 'cURL' command straight into a new download.

```bash
# Start the TUI
surge

# Start the TUI without the local HTTP API server
surge --no-server

# Start TUI with downloads queued
surge https://example.com/file1.zip https://example.com/file2.zip

# Combine URLs and batch file
surge https://example.com/file.zip --batch urls.txt
```

`--no-server` keeps the TUI fully local and skips the embedded HTTP API. CLI control commands such as `surge add`, `surge pause`, and browser-extension requests will not be able to target that instance.

### 2. Server Mode (Headless)

Great for servers, Raspberry Pis, or background processes.

```bash
# Start the server
surge server

# Start the server with a download
surge server https://url.com/file.zip

# Start with explicit API token
surge server --token <token>
```

### 3. Auto-Start Service

Surge provides an official way to manage it as a system service (daemon). This is the recommended way for servers and reproducible deployments.

```bash
# Install Surge as a system service
surge service install

# Manage the service
surge service start
surge service stop
surge service status
surge service token

# Uninstall the service
surge service uninstall
```

> [!NOTE]
> On Linux, these commands may require `sudo`. On Windows, they should be run in an elevated (Administrator) terminal.

### 4. Remote TUI

`surge` and `surge server` bind the HTTP API to `0.0.0.0` (all interfaces) by default.
This means the server is accessible via `localhost` (127.0.0.1) as well as your local network IP.

The API is token-protected. Generate/read your token by running:

```bash
# Get the standard token
surge token

# Get the token if Surge is installed as a system service
surge service token
```

Alternatively, you can find it in the TUI under **Settings > Extension**.

### 3. Remote TUI

Connect to a running Surge daemon (local or remote).

```bash
# Connect to local server (auto-detected)
surge connect

# Connect to a remote daemon
surge connect 192.168.1.10:1700 --token <token>

# Equivalent global-flag form
surge --host 192.168.1.10:1700 --token <token>
```

By default, `surge connect` uses:

- `http://` for loopback and private IP targets
- `https://` for public/hostname targets

### 4. Global Connection Flags (CLI + TUI)

These global flags are available on all commands:

- `--host <host:port>`: target server for TUI and CLI operations.
- `--token <token>`: bearer token for authentication.

Environment variable fallbacks:

- `SURGE_HOST`
- `SURGE_TOKEN`

### 5. Server Mode with Docker Compose

Download the compose file and start the container:

```bash
wget https://raw.githubusercontent.com/SurgeDM/Surge/refs/heads/main/docker/compose.yml
docker compose up -d
```

Get the API token:

```bash
docker compose exec surge surge token
```

Save this token - you'll need it to authenticate API requests and connect remotely.

Check downloads/API availability:

```bash
docker compose exec surge surge ls
```

View logs:

```bash
docker compose logs -f surge
```

---

## Fonts

Surge ships a bundled Nerd Font for the TUI, but your terminal controls the
actual font selection. See the [customization guide](docs/guides/customize-surge.md) for install steps and
licensing details.

---

## Benchmarks

We tested Surge against standard tools. Because of our connection optimization logic, Surge significantly outperforms single-connection tools.

| Tool            | Time             | Speed                | Comparison    |
| --------------- | ---------------- | -------------------- | ------------- |
| **Surge** | **28.93s** | **35.40 MB/s** | **-**  |
| aria2c          | 40.04s           | 25.57 MB/s           | 1.38× slower |
| curl            | 57.57s           | 17.79 MB/s           | 1.99× slower |
| wget            | 61.81s           | 16.57 MB/s           | 2.14× slower |

> _Test details: 1GB file, Windows 11, Ryzen 5 5600X, 360 Mbps Network. Results averaged over 5 runs._

We would love to see you benchmark Surge on your system!

To compare fixed and adaptive concurrency under deterministic throttling, run:

```bash
go test ./internal/strategy/concurrent -run '^$' -bench BenchmarkThrottle -benchtime=1x -count=5
```

The native Go benchmark runs persistent-overload and burst-recovery workloads
with both policies and reports elapsed time, request amplification, throttled
requests, and peak accepted concurrency.

---

## Browser Extension

The Surge extension intercepts browser downloads and sends them straight to your terminal. It communicates with the Surge client on port **1700** by default.

> [!IMPORTANT]
> An **Auth Token** is required to connect the extension to your Surge server. This can be obtained from the TUI under **Settings > Extension**, or by running `surge token` (or `surge service token` if installed as a system service).

### Chrome / Edge / Brave

1. **Stable:** [Get the Extension from Chrome Web Store](https://chromewebstore.google.com/detail/surgedm/cakjmkhlofkhjmfkjlclgbfdklhdnkgl)
2. **Development:**
   - Download `extension-chrome.zip` from the latest GitHub release.
   - Unzip it somewhere on disk.
   - Open your browser and navigate to `chrome://extensions`.
   - Enable **"Developer mode"** in the top right corner.
   - Click **"Load unpacked"**.
   - Select the unzipped `extension-chrome` folder.
   - Click the Surge icon in your browser toolbar and enter your **Auth Token** in the settings.

### Firefox

1. **Stable:** [Get the Add-on](https://addons.mozilla.org/en-US/firefox/addon/surge/)
2. **Development:**
   - Download `extension-firefox.zip` from the latest GitHub release.
   - Navigate to `about:debugging#/runtime/this-firefox`.
   - Click **"Load Temporary Add-on..."**.
   - Select the zip file (or unzip and select `manifest.json`).
   - Click the Surge icon in your browser toolbar and enter your **Auth Token** in the settings.

---

## Acknowledgements

Huge thanks to the teams and sponsors helping us build and ship Surge:

- [FOSS United](https://fossunited.org/) for their generous grant supporting our development. We genuinely love and appreciate everything they do to build and nurture the FOSS ecosystem in India.
- [Charm](https://charm.sh/) for the incredible terminal UI ecosystem (Bubble Tea, Lip Gloss, and more).
- [GoReleaser Pro](https://goreleaser.com/pro/) for release automation (provided free for open source).

---

## Community & Contributing

We love community contributions! Whether it's a bug fix, a new feature, or just cleaning up typos.
PRs are always welcome. For a quick guide, see [CONTRIBUTING.md](CONTRIBUTING.md).

You can check out the [Discussions](https://github.com/SurgeDM/Surge/discussions) for any questions or ideas, or follow us on [X (Twitter)](https://x.com/SurgeDownloader)!

## License

Distributed under the MIT License. See [LICENSE](https://github.com/SurgeDM/Surge/blob/main/LICENSE) for more information.

---

<div align="center">
<a href="https://star-history.dera.page/#SurgeDM/Surge&Date">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://star-history.dera.page/svg?repos=SurgeDM/Surge&type=Date&theme=dark" />
   <source media="(prefers-color-scheme: light)" srcset="https://star-history.dera.page/svg?repos=SurgeDM/Surge&type=Date" />
   <img alt="Star History Chart" src="https://star-history.dera.page/svg?repos=SurgeDM/Surge&type=Date" />
 </picture>
</a>

<br />
If Surge saved you some time, consider giving it a ⭐ to help others find it!
</div>

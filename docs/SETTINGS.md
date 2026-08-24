# Settings & Configuration

This document covers all configuration options available in Surge. For CLI commands and flags, see [USAGE.md](USAGE.md).

## Configuration File

You can **access the settings in TUI** or configure them via the CLI tool `surge config`. 
If you prefer manual editing, you can modify the `settings.toml` file located in the application data directory:

- **Windows:** `%APPDATA%\surge\settings.toml`
- **macOS:** `~/Library/Application Support/surge/settings.toml`
- **Linux:** `~/.config/surge/settings.toml`

The `settings.toml` file expects a structure divided into `[general]`, `[network]`, `[performance]`, and `[categories]` sections. For example:

```toml
[general]
default_download_dir = "/path/to/downloads"
theme = 2

[network]
max_connections_per_host = 32

[performance]
max_task_retries = 5

[categories]
category_enabled = true
```

*Note: You do not need to specify all keys. Surge will automatically infer missing keys and use their internal default values.*

## Configuration Validation

Surge implements a self-healing configuration system to ensure the application remains stable even if the `settings.toml` file is manually edited with invalid values.

- **Range Enforcement**: Numeric values (like connection limits and concurrent downloads) are strictly enforced. If a value is set outside its safe operating range in the TOML file, Surge will automatically reset that specific field to its default value on startup while preserving your other valid settings.
- **Path Verification**: Paths like `default_download_dir` and individual category paths are verified for existence and accessibility. Broken or inaccessible paths are rolled back to the system's default Downloads directory.
- **Syntactic Validation**: Proxy URLs and DNS server lists are validated for correct syntax.
- **Category Integrity**: If a custom category has an invalid regular expression pattern, it is automatically pruned from the active list to prevent engine crashes.
- **Corrupt TOML Fallback**: If the `settings.toml` file is completely unparseable, Surge will log a warning and start with all factory default settings for that session.
## Keymap Configuration

Surge allows you to customize your keyboard shortcuts by editing the `keymap.json` file located in the application config directory:

- **Windows:** `%APPDATA%\surge\keymap.json`
- **macOS:** `~/Library/Application Support/surge/keymap.json`
- **Linux:** `~/.config/surge/keymap.json`

Surge will automatically generate this file with all default keybindings (including Vim-style keys) on the first startup.

### Structure

The `keymap.json` file is structured into nested sections matching each TUI state (e.g., `dashboard`, `settings`, `file_picker`, etc.). Each binding consists of an array of key strings and a help description. For example:

```json
{
  "dashboard": {
    "Quit": {
      "keys": [
        "ctrl+c",
        "ctrl+q"
      ],
      "help": "quit"
    },
    "Up": {
      "keys": [
        "up",
        "k"
      ],
      "help": "up"
    }
  }
}
```

*Note: You do not need to specify all keys. Surge will automatically validate and fall back to internal defaults for any missing or invalid keybindings on startup.*

### Ambiguous Bindings & ForceQuit

By default, both `Quit` and `ForceQuit` in the dashboard bind `ctrl+c`. 
- **Quit** (`ctrl+c` or `ctrl+q`) initiates a graceful shutdown of all active download tasks, ensuring that progress and state are fully persisted before exiting the application.
- **ForceQuit** (`ctrl+c`) performs an immediate exit of the application without waiting for the graceful shutdown of the background download engine.

If you choose to customize these bindings, you can separate them (e.g. binding `Quit` exclusively to `ctrl+q` and `ForceQuit` exclusively to `ctrl+c`) to avoid any ambiguity during normal exit.

## Directory Structure

Surge follows OS conventions for storing its files. Below is a breakdown of every directory it uses and where to find it on each platform.

| Directory   | Purpose                           | Linux                        | macOS                                       | Windows                 |
| :---------- | :-------------------------------- | :--------------------------- | :------------------------------------------ | :---------------------- |
| **Config**  | `settings.toml`, `keymap.json`    | `~/.config/surge/`           | `~/Library/Application Support/surge/`      | `%APPDATA%\surge\`      |
| **State**   | Database (`surge.db`), auth token | `~/.local/state/surge/`      | `~/Library/Application Support/surge/`      | `%APPDATA%\surge\`      |
| **Logs**    | Timestamped `.log` files          | `~/.local/state/surge/logs/` | `~/Library/Application Support/surge/logs/` | `%APPDATA%\surge\logs\` |
| **Themes**  | Custom `.toml` theme files        | `~/.config/surge/themes/`    | `~/Library/Application Support/surge/themes/` | `%APPDATA%\surge\themes\` |
| **Runtime** | PID file, port file, lock         | `$XDG_RUNTIME_DIR/surge/`¹   | `$TMPDIR/surge-runtime/`                    | `%TEMP%\surge\`         |

> ¹ Falls back to `~/.local/state/surge/` when `$XDG_RUNTIME_DIR` is not set (e.g. Docker / headless).

> **Note:** On Linux, `$XDG_CONFIG_HOME` / `$XDG_STATE_HOME` are respected if set; the paths above show the defaults.

---

### General Settings

| Key                    | Type   | Description                                                                                        | Default |
| :--------------------- | :----- | :------------------------------------------------------------------------------------------------- | :------ |
| `default_download_dir` | string | Directory where new downloads are saved. If empty, defaults to `~/Downloads` or current directory. | `""`    |
| `allow_remote_open_actions` | bool | Allow `/open-file` and `/open-folder` API requests from remote clients. Keep disabled unless you trust your network and auth setup. | `false` |
| `warn_on_duplicate`    | bool   | Show a warning when adding a download that already exists in the list.                             | `true`  |
| `extension_prompt`     | bool   | Prompt for confirmation in the TUI when adding downloads via the browser extension.                | `false` |
| `auto_resume`          | bool   | Automatically resume paused downloads when Surge starts.                                           | `false` |
| `auto_start`           | bool   | Automatically start Surge as a system service on boot. (See [USAGE.md](USAGE.md#service-management)).      | `false` |
| `skip_update_check`    | bool   | Disable automatic check for new versions on startup.                                               | `false` |
| `clipboard_monitor`    | bool   | Watch the system clipboard for URLs and prompt to download them.                                   | `true`  |
| `theme`                | int    | UI Theme (0=Adaptive, 1=Light, 2=Dark).                                                            | `0`     |
| `theme_path`           | string | Path to a custom `.toml` color scheme or name of theme in the `themes` directory. See [THEMES.md](THEMES.md). | `""`    |
| `log_retention_count`  | int    | Number of recent log files to keep.                                                                | `5`     |
| `live_speed_graph`     | bool   | Use live speed for graph instead of EMA smoothed speed.                                            | `false` |

### Connection Settings

| Key                        | Type   | Description                                                                                           | Default |
| :------------------------- | :----- | :---------------------------------------------------------------------------------------------------- | :------ |
| `max_connections_per_host` | int    | Maximum concurrent connections per download (1-64). High values may trigger server rate limits. | `32`    |
| `max_concurrent_downloads` | int    | Maximum number of downloads running simultaneously (requires restart).                                | `3`     |
| `global_rate_limit`        | string | Global speed limit across all downloads (e.g. `10 MB/s`, `0` or `∞` for unlimited).                   | `0`     |
| `default_download_rate_limit` | string | Default speed limit applied to new downloads (e.g. `5 MB/s`, `0` or `∞` for unlimited).            | `0`     |
| `max_concurrent_probes`    | int    | Maximum number of simultaneous server probes when many downloads are added at once (1-10). Requires restart. | `3`     |
| `user_agent`               | string | Custom User-Agent string for HTTP requests. Leave empty for default.                                  | `""`    |
| `proxy_url`                | string | HTTP/HTTPS proxy URL (e.g., `http://127.0.0.1:8080`). Leave empty to use system settings.             | `""`    |
| `sequential_download`      | bool   | Download file pieces in strict order (Streaming Mode). Useful for previewing media but may be slower. | `false` |
| `min_chunk_size`           | int64  | Minimum size of a download chunk in bytes (e.g., `2097152` for 2MB).                                  | `2MB`   |
| `worker_buffer_size`       | int    | I/O buffer size per worker in bytes (e.g., `524288` for 512KB).                                       | `512KB` |
| `dial_hedge_count`         | int    | Extra prewarmed connections (0-16). Set to 0 to disable prewarming. | `4`     |

### Performance Settings

| Key                        | Type     | Description                                                                  | Default |
| :------------------------- | :------- | :--------------------------------------------------------------------------- | :------ |
| `max_task_retries`         | int      | Number of times to retry a failed chunk before giving up.                    | `3`     |
| `slow_worker_threshold`    | float    | Restart workers slower than this fraction of the mean speed (0.0-1.0).       | `0.3`   |
| `slow_worker_grace_period` | duration | Time to wait before checking a worker's speed (e.g., `5s`).                  | `5s`    |
| `stall_timeout`            | duration | Restart workers that haven't received data for this duration (e.g., `3s`).   | `3s`    |
| `speed_ema_alpha`          | float    | Exponential moving average smoothing factor for speed calculation (0.0-1.0, 0 disables smoothing). | `0.3`   |
| `adaptive_concurrency_interval` | duration | Halve connections when throttled, add one connection per interval, and split small tail ranges among idle workers on healthy hosts. Set to `0` to disable adaptation. | `0` |

### Category Settings

| Key                    | Type   | Description                                                                                              | Default |
| :--------------------- | :----- | :------------------------------------------------------------------------------------------------------- | :------ |
| `category_enabled`     | bool   | Enable automatic sorting of downloads into subfolders based on file type categories.                     | `false` |

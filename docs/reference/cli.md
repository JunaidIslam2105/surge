# Command-line reference

This is the stable index for Surge commands. Run `surge <command> --help` for
the exact flags in the installed release.

## Command guides

These pages show complete workflows, including prerequisites and recovery
steps. They should be your starting point when you know what you want to do.

- [`surge add` and `surge get`](commands/add.md)
- [`surge server`](commands/server.md)
- [Inspect and manage downloads: `ls`, `pause`, `resume`, `refresh`, `limit`, and `rm`](commands/inspect-and-manage.md)
- [`surge connect`](commands/connect.md)
- [`surge service`](commands/service.md)
- [`surge config`](commands/config.md)
- [Tokens, shell completion, and bug reports](commands/utilities.md)

## Command index

| Command | Purpose |
| --- | --- |
| `surge [url]...` | Open the local TUI and optionally queue URLs. |
| [`surge server [url]...`](commands/server.md) | Run the headless download server. `server start` is a compatible explicit form. |
| [`surge connect [host:port]`](commands/connect.md) | Open the TUI against a running server. |
| [`surge add <url>...` / `surge get`](commands/add.md) | Add URLs to a running server. |
| [`surge ls`, pause, resume, refresh, limit, and rm](commands/inspect-and-manage.md) | Inspect or manage downloads. `rm --purge` also deletes files from disk. |
| [`surge config [path] [value]`](commands/config.md) | List, read, set, reset, or open settings. |
| [`surge token`, `completion`, and `bug-report`](commands/utilities.md) | Print a token, generate shell completion, or open the issue flow. |
| [`surge service`](commands/service.md) | Install and manage a system service. |

See [Download and manage files](../guides/download-files.md) for examples and
[Run Surge as a service](../guides/run-as-a-service.md) for service commands.

## Root command flags

| Flag | Meaning |
| --- | --- |
| `--batch`, `-b` | Read URLs from a file, one per line. |
| `--output`, `-o` | Choose the download directory; defaults to the current directory. |
| `--port`, `-p` | Choose the listening port; the root command defaults to 8080 or the first available port. |
| `--no-resume` | Do not resume paused downloads at startup. |
| `--exit-when-done` | Exit after all downloads complete. |
| `--no-server` | Run the TUI without its HTTP API server. |
| `--reset-settings` | Reset settings and keybindings at startup. |

## Remote connection flags

These persistent flags work with the TUI and control commands.

| Flag | Meaning |
| --- | --- |
| `--host` | Target a server; `SURGE_HOST` provides the same default. |
| `--token` | Supply the API bearer token; `SURGE_TOKEN` provides the same default. |
| `--insecure-http` | Allow plain HTTP for a non-private remote target. |
| `--insecure-tls` | Skip TLS certificate verification. |
| `--tls-ca-file` | Trust certificates signed by the supplied PEM bundle. |

The two `insecure` flags reduce connection security. See [Connect to a remote
server](../guides/connect-to-a-remote-server.md) before using them.

## Configuration command

Use the case-sensitive setting path shown by `surge config`:

```bash
surge config Network.Max_Concurrent_Downloads
surge config Network.Max_Concurrent_Downloads 4
surge config Network.Max_Concurrent_Downloads default
surge config open
```

For setting meanings and configuration-file locations, see the
[configuration reference](../SETTINGS.md).

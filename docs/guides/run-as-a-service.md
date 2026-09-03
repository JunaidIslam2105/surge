# Run Surge as a system service

Install a system service when Surge should keep running after you close your
terminal or start automatically with the machine. The service runs the headless
server, not the interactive TUI.

## Before you install

Start Surge manually first and make sure it can download to the intended
location. On Linux and Windows, service management may require `sudo` or an
Administrator terminal.

## Install and start

```bash
surge service install
surge service start
surge service status
```

The service has its own authentication token. Retrieve it when connecting a
remote TUI or configuring the browser extension:

```bash
surge service token
```

Reading the system token may require elevated privileges.

## Control the service

Use the normal commands once the service is running:

```bash
surge add https://example.com/archive.zip
surge ls
surge pause --all
```

If the local client cannot discover the service token, provide it explicitly
with `--token` or set `SURGE_TOKEN`.

## Stop or remove the service

```bash
surge service stop
surge service uninstall
```

Uninstalling the service does not intentionally delete your completed downloads.
Check [data locations](../SETTINGS.md#directory-structure) before manually
removing configuration or state files.

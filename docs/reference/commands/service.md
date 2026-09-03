# `surge service`

Manage Surge as a system service. A service runs the headless server in the
background and is suitable for a home server, workstation, or other machine
that should keep its queue after you close the terminal.

## Install the service

Run this from an elevated terminal where required:

```bash
surge service install
```

Surge creates a dedicated token for the system service. This token is separate
from the token used by an interactive, per-user Surge server.

## Start and check the service

```bash
surge service start
surge service status
```

Once it is running, normal control commands target the server:

```bash
surge add https://example.com/archive.zip
surge ls
```

## Retrieve the service token

Use the service token when connecting a remote TUI or the browser extension:

```bash
surge service token
```

The token may be protected by the operating system. On Linux, retry with
`sudo` when the command cannot read it.

## Stop or uninstall

```bash
surge service stop
surge service uninstall
```

Uninstalling removes the system service. It does not intentionally remove
downloaded files; inspect your [Surge data locations](../../SETTINGS.md#directory-structure)
before removing configuration or state manually.

## When to use a service

Use `surge service` when the engine should run independently of a terminal.
For a short, foreground batch download, prefer:

```bash
surge server --batch urls.txt --exit-when-done
```

For the full remote-control flow, see [Connect to a remote server](../../guides/connect-to-a-remote-server.md).

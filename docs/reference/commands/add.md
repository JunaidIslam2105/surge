# `surge add`

Add one or more URLs to a running Surge server without opening the TUI. `get`
is an alias for this command.

```text
surge add [url]...
surge get [url]...
```

## Before you begin

A local TUI or headless server must already be running. Start one in another
terminal if needed:

```bash
surge
# or, without the TUI
surge server
```

If the TUI was started with `--no-server`, `add` cannot connect to it.

## Add one URL

```bash
surge add https://example.com/releases/surge.tar.gz
```

Surge adds the URL to the running server's queue. Use `surge ls` to find its ID
and monitor the result.

## Save to a specific directory

```bash
surge add https://example.com/releases/surge.tar.gz --output ~/Downloads
```

The output directory defaults to the current working directory. Quote paths
that contain spaces:

```powershell
surge add https://example.com/releases/surge.tar.gz --output "$HOME\My Downloads"
```

## Add several URLs

Pass URLs as separate arguments:

```bash
surge add \
  https://example.com/one.iso \
  https://example.com/two.iso
```

For a longer list, create a plain-text file with one URL per line and use
`--batch`:

```text
# urls.txt
https://example.com/one.iso
https://example.com/two.iso
```

```bash
surge add --batch urls.txt
```

## Review before queuing

Use `--confirm` when a batch file or pasted list needs a final check:

```bash
surge add --batch urls.txt --confirm
```

## Add to a remote server

Supply the remote host and its token before the command:

```bash
surge --host https://192.168.1.10:1700 --token "$SURGE_TOKEN" add \
  https://example.com/releases/surge.tar.gz
```

Remote connections that send a token should use HTTPS. Set `SURGE_HOST` and
`SURGE_TOKEN` when you use the same server regularly. See
[Connect to a remote server](../../guides/connect-to-a-remote-server.md) for
connection and security guidance.

## Related commands

- [`surge ls`](inspect-and-manage.md#surge-ls) shows queued downloads and IDs.
- [`surge pause`, `resume`, and `refresh`](inspect-and-manage.md) control a
  queued download.

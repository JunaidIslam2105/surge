# Download and manage files

Use this guide when Surge is already running locally or when you know which
server you want to control.

## Queue a download

Start the TUI and queue a URL in one step:

```bash
surge https://example.com/video.mp4
```

To add work to a running Surge server without opening the TUI, use `add` (or
its alias, `get`):

```bash
surge add https://example.com/video.mp4
surge get https://example.com/audio.flac --output ~/Downloads
```

Use `--confirm` to review the queued URLs before they start. To queue one URL
per line from a file, use `--batch`:

```bash
surge add --batch urls.txt
```

## Inspect downloads

List downloads with their IDs:

```bash
surge ls
```

Pass an ID, or an unambiguous ID prefix, to inspect one download. Use `--json`
when another program needs structured output, and `--watch` to refresh the list
once per second.

```bash
surge ls ab12
surge ls --json
surge ls --watch
```

## Pause, resume, and refresh

```bash
surge pause ab12
surge resume ab12
```

Use `--all` with `pause` or `resume` to affect every eligible download. If an
expired or changed URL leaves a download paused or in an error state, update its
source before resuming it:

```bash
surge refresh ab12 https://example.com/replacement-url
surge resume ab12
```

## Control speed

Set a limit for one download, for all current downloads, or as the default for
new downloads. Use `0` or `unlimited` to remove a limit.

```bash
surge limit ab12 10MiB
surge limit --global 50MiB
surge limit --default unlimited
```

Run `surge limit --help` for the accepted speed formats and full command
syntax.

## Remove downloads

Removing a download only removes it from Surge's queue by default; it does not
delete the downloaded file.

```bash
surge rm ab12
surge rm --clean
surge rm --clean-failed
```

> [!WARNING]
> `surge rm <ID> --purge` also deletes the downloaded file or files from disk.
> Check the ID and destination before using it.

## Control another server

Add `--host` and an authentication token to any control command:

```bash
surge --host 192.168.1.10:1700 --token "$SURGE_TOKEN" ls
```

For repeated use, set `SURGE_HOST` and `SURGE_TOKEN` in your environment. See
[Connect to a remote server](connect-to-a-remote-server.md).

# `surge server`

Run Surge without the interactive TUI. The server owns the download queue and
exposes the authenticated API used by `surge add`, other control commands, and
the browser extension.

```text
surge server [url]...
surge server start [url]...
```

`surge server start` is the explicit form of the same headless startup command.

## Start a server

```bash
surge server
```

Keep this process running. From another terminal, add work and inspect it:

```bash
surge add https://example.com/archive.zip
surge ls
```

## Queue files at startup

```bash
surge server https://example.com/one.iso https://example.com/two.iso
```

Use a batch file for longer lists:

```bash
surge server --batch urls.txt --output /srv/downloads
```

`urls.txt` contains one URL per line. The output directory defaults to the
directory where you start Surge.

## Choose a port and token

The root command uses port 8080 or the first available port by default. Choose a
fixed port when clients need a predictable address:

```bash
surge server --port 1700 --token "$SURGE_TOKEN"
```

If you do not provide a token, Surge manages one for its local use. Print it on
the server with:

```bash
surge token
```

Treat this token as a password. It grants control over the queue.

## Start, stop, or check a directly managed server

```bash
surge server start
surge server status
surge server stop
```

For a server that should survive logout and boot with the machine, use
[`surge service`](service.md) instead.

## Exit after work completes

For a one-off batch job, stop the headless server once the queue has completed:

```bash
surge server --batch urls.txt --exit-when-done
```

Add `--no-resume` when you do not want paused downloads restored on startup.

## Connect from another machine

Use a remote TUI:

```bash
surge connect 192.168.1.10:1700 --token "$SURGE_TOKEN"
```

Read [Connect to a remote server](../../guides/connect-to-a-remote-server.md)
before exposing a server outside a private network.

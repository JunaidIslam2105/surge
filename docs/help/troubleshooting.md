# Troubleshooting

Start with the smallest check that can explain the problem. Include the Surge
version, operating system, command used, and a redacted log when reporting an
issue. Never include an API token, a cURL command containing cookies, or private
download URLs.

## A command cannot reach Surge

Commands such as `surge add` and `surge ls` control a running local or remote
server. Start one first:

```bash
surge
# or
surge server
```

If you started the TUI with `--no-server`, restart it without that option.
For remote use, verify the address, port, and token:

```bash
surge --host 192.168.1.10:1700 --token "$SURGE_TOKEN" ls
```

## A remote connection returns an authentication error

Get the token from the machine that runs the server:

```bash
surge token
```

For a system service, use `surge service token`; it may need elevated
privileges. Then pass the token with `--token` or set `SURGE_TOKEN`.

## A remote connection is refused or times out

Check that the server is running and that the host and port are reachable from
the client. On the server, run:

```bash
surge server status
surge service status
```

If you use a public hostname, configure HTTPS. Do not solve certificate errors
by permanently adding `--insecure-tls`; use `--tls-ca-file` for a trusted
private CA instead.

## A download fails after its URL expires

Pause the download if necessary, replace the URL, then resume it:

```bash
surge refresh <id> <new-url>
surge resume <id>
```

## Settings do not take effect

Check the setting path and value with `surge config`. When configuration files
contain invalid values, Surge validates them and can fall back to a safe default
on startup. Review [Configuration validation](../SETTINGS.md#configuration-validation)
and the configuration-file path for your operating system.

## The TUI glyphs look wrong

Install and select `JetBrainsMono Nerd Font Mono` in your terminal emulator.
See [Font installation](../FONTS.md).

## Still stuck?

Use `surge bug-report` to open the guided issue flow, or file an issue at
[github.com/SurgeDM/Surge/issues](https://github.com/SurgeDM/Surge/issues).

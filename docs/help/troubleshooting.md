# Troubleshooting

Start with the smallest check that can explain the problem. Include the Surge
version, operating system, command used, and a redacted log when reporting an
issue. Never include an API token, a cURL command containing cookies, or private
download URLs.

## A command cannot reach Surge

Commands that change the queue, such as `surge add`, need a running local or
remote server. `surge ls` can inspect the local database when no server is
running, but it cannot show live progress in that case. Start a server when you
need live control:

```bash
surge
# or
surge server
```

If you started the TUI with `--no-server`, restart it without that option.
For remote use, verify the address, port, and token:

```bash
surge --host https://192.168.1.10:1700 --token "$SURGE_TOKEN" \
  --tls-ca-file ./surge-ca.pem ls
```

Use `--tls-ca-file` only when the remote HTTPS endpoint uses a private or
self-signed CA; otherwise use the system trust store. Do not send a token to an
HTTP remote endpoint.

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

Check the setting path and value with `surge config`. Individual invalid values
are validated and reset to their safe defaults. If `settings.toml` is corrupt or
cannot be parsed, Surge starts with `config.DefaultSettings()` for every setting
and records a startup warning about the full fallback. Review [Configuration
validation](../SETTINGS.md#configuration-validation) and the configuration-file
path for your operating system.

## The TUI glyphs look wrong

Install and select `JetBrainsMono Nerd Font Mono` in your terminal emulator.
See [Font installation](../FONTS.md).

## Still stuck?

Use `surge bug-report` to open the guided issue flow, or file an issue at
[github.com/SurgeDM/Surge/issues](https://github.com/SurgeDM/Surge/issues).

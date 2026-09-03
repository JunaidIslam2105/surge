# Choose a Surge mode

Surge uses one download engine in three different ways. Pick the mode based on
where the engine runs and how you intend to control it.

| Mode | Start it with | Best for |
| --- | --- | --- |
| Interactive TUI | `surge` | Managing downloads directly in a terminal |
| Headless server | `surge server` | Servers, automation, and browser-extension integration |
| Remote TUI | `surge connect host:port` | Managing a running server from another terminal |

## Interactive TUI

Running `surge` opens the terminal interface and starts a local API server by
default. That local API lets commands such as `surge add` and `surge pause`, as
well as the browser extension, control the same instance.

Use `--no-server` only when you explicitly want a local-only TUI. With that
flag, CLI control commands and browser-extension requests cannot connect to the
instance.

## Headless server

`surge server` runs the engine without the TUI. It is the right mode for a
remote machine, Docker, scripts, and long-running background use. Queue URLs at
startup or add them later:

```bash
surge server https://example.com/large-file.iso
surge add https://example.com/another-file.iso
```

The API requires an authentication token. Print the token for a running local
server with `surge token`.

## Remote TUI

`surge connect` opens the same TUI against a server that is already running.
When no address is supplied, it looks for a local server. For a remote server,
provide the address and token:

```bash
surge connect https://192.168.1.10:1700 --token "$SURGE_TOKEN"
```

Use HTTPS for every remote connection that sends a bearer token. Surge currently
selects HTTP automatically for loopback and private IP addresses, but that sends
the resolved token without transport confidentiality. Use plain HTTP only for a
local loopback connection. Put a remote Surge server behind a trusted HTTPS
endpoint; use `--tls-ca-file` when that endpoint uses a private CA. Do not send
tokens to HTTP endpoints or through redirects, and do not use `--insecure-http`
or `--insecure-tls` outside a controlled test environment.

For setup and security notes, see [Connect to a remote server](../guides/connect-to-a-remote-server.md).

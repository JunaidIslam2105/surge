# Connect to a remote Surge server

Use a remote connection when the download engine runs on another machine. The
server needs to be running first:

```bash
surge server
```

## Connect with the TUI

On the server, retrieve the token:

```bash
surge token
```

On your client machine, connect with the host, port, and token:

```bash
surge connect 192.168.1.10:1700 --token "your-token"
```

You can also use a full URL. Surge defaults to HTTP for private and loopback
addresses, and to HTTPS for public addresses and hostnames.

```bash
surge connect https://downloads.example.com:1700 --token "$SURGE_TOKEN"
```

## Control the server without the TUI

Use `--host` with any CLI control command:

```bash
surge --host 192.168.1.10:1700 --token "$SURGE_TOKEN" ls
surge --host 192.168.1.10:1700 --token "$SURGE_TOKEN" add https://example.com/file.zip
```

Set these environment variables if you use the same server often:

```bash
export SURGE_HOST=192.168.1.10:1700
export SURGE_TOKEN=your-token
```

## Keep the connection secure

- Do not expose a Surge server directly to the public internet without HTTPS
  and network access controls.
- Keep tokens out of shell history, screenshots, repositories, and issue
  reports. Prefer `SURGE_TOKEN` to placing a token in a copied command.
- Use `--tls-ca-file` when your server uses a private certificate authority.
- `--insecure-http` permits plain HTTP to public targets, and `--insecure-tls`
  skips certificate verification. Both weaken the connection; use them only for
  a trusted test or private network where you understand the risk.

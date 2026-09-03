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

On your client machine, securely prompt for the token, then connect with the
host and port:

```bash
read -rsp 'Surge token: ' SURGE_TOKEN; echo
export SURGE_TOKEN
surge connect https://192.168.1.10:1700 --token "$SURGE_TOKEN"
```

You can also use a full URL. Surge defaults to HTTP for private and loopback
addresses, and to HTTPS for public addresses and hostnames. That automatic HTTP
choice sends the resolved `Authorization` bearer token without transport
confidentiality, so do not use it for a remote connection. Put the server behind
an HTTPS endpoint instead.

```bash
surge connect https://downloads.example.com:1700 --token "$SURGE_TOKEN"
```

If that endpoint uses a private or self-signed CA, trust its PEM bundle instead
of disabling certificate verification:

```bash
surge connect https://downloads.example.com:1700 \
  --token "$SURGE_TOKEN" \
  --tls-ca-file ./surge-ca.pem
```

## Control the server without the TUI

Use `--host` with any CLI control command:

```bash
surge --host https://192.168.1.10:1700 --token "$SURGE_TOKEN" ls
surge --host https://192.168.1.10:1700 --token "$SURGE_TOKEN" add https://example.com/file.zip
```

Set these environment variables if you use the same server often:

```bash
export SURGE_HOST=https://192.168.1.10:1700
read -rsp 'Surge token: ' SURGE_TOKEN; echo
export SURGE_TOKEN
```

## Keep the connection secure

- Do not expose a Surge server directly to the public internet. Terminate HTTPS
  at a trusted endpoint and apply network access controls.
- Bearer tokens must not travel over HTTP or be forwarded by redirects. Keep
  them out of shell history, screenshots, repositories, and issue reports.
- Use `--tls-ca-file` when your HTTPS endpoint uses a private or self-signed CA.
- `--insecure-http` permits plain HTTP to public targets, and `--insecure-tls`
  skips certificate verification. Both weaken the connection; use them only for
  a trusted test or private network where you understand the risk.

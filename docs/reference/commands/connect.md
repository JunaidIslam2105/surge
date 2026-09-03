# `surge connect`

Open the interactive TUI against a running Surge server.

```text
surge connect [host:port]
```

## Connect to a local server

With no address, Surge looks for a locally running server:

```bash
surge connect
```

Start `surge` or `surge server` first if no local server is detected.

## Connect to a private-network server

```bash
surge connect 192.168.1.10:1700 --token "$SURGE_TOKEN"
```

For loopback and private IP addresses, Surge uses HTTP automatically unless a
URL specifies another scheme.

## Connect to a public hostname

```bash
surge connect https://downloads.example.com:1700 --token "$SURGE_TOKEN"
```

Surge uses HTTPS automatically for public addresses and hostnames. If your
server uses a private CA, provide its PEM bundle:

```bash
surge connect https://downloads.example.com:1700 \
  --token "$SURGE_TOKEN" \
  --tls-ca-file ./company-ca.pem
```

Do not use `--insecure-http` or `--insecure-tls` as a permanent workaround for
an untrusted connection. See [remote connection security](../../guides/connect-to-a-remote-server.md#keep-the-connection-secure).

## Avoid repeating credentials

Set defaults for a trusted server in your shell environment:

```bash
export SURGE_HOST=192.168.1.10:1700
export SURGE_TOKEN=your-token
surge connect
```

The same values are used by `surge add`, `surge ls`, and other remote control
commands.

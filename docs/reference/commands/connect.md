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
surge connect https://192.168.1.10:1700 --token "$SURGE_TOKEN"
```

For loopback and private IP addresses, Surge uses HTTP automatically unless a
URL specifies another scheme. Do not use that automatic HTTP mode for a remote
connection: it sends the resolved bearer token without transport confidentiality.
Use HTTPS for remote connections, including private-network servers.

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

Set the server address, then securely prompt for its token in your shell:

```bash
export SURGE_HOST=https://192.168.1.10:1700
read -rsp 'Surge token: ' SURGE_TOKEN; echo
export SURGE_TOKEN
surge connect
```

The same values are used by `surge add`, `surge ls`, and other remote control
commands. If the HTTPS endpoint uses a private or self-signed CA, add
`--tls-ca-file ./surge-ca.pem` to the command.

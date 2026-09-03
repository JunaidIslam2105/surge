# Inspect and manage downloads

These commands operate on the queue in a running Surge server. Use `surge ls`
first to find a download ID; an unambiguous ID prefix is accepted where an ID is
required.

## `surge ls`

List all downloads:

```bash
surge ls
```

Show details for one download:

```bash
surge ls ab12
```

Use JSON when a script needs structured output, or watch the queue in a terminal:

```bash
surge ls --json
surge ls --watch
```

`l` is an alias for `ls`.

## `surge pause`

Pause one download:

```bash
surge pause ab12
```

Pause all downloads:

```bash
surge pause --all
```

## `surge resume`

Resume a paused download:

```bash
surge resume ab12
```

Resume every paused download:

```bash
surge resume --all
```

## `surge refresh`

Refresh replaces the source URL of a paused or failed download. This is useful
when a signed URL has expired or the original link no longer works.

```bash
surge refresh ab12 https://example.com/new-signed-url
surge resume ab12
```

The download must be paused or in an error state before it can be refreshed.

## `surge limit`

Set a cap for one download:

```bash
surge limit ab12 2MB/s
```

Set a global cap for the active queue:

```bash
surge limit --global 10MB/s
```

Set the default cap for new downloads:

```bash
surge limit --default 2MB/s
```

Use `0` to make an individual download unlimited. Use `-1` to make it inherit
the default per-download limit.

## `surge rm`

Remove one record from Surge's queue without deleting the downloaded file:

```bash
surge rm ab12
```

Clean up completed or failed records:

```bash
surge rm --clean
surge rm --clean-failed
```

> [!WARNING]
> `surge rm ab12 --purge` also deletes the corresponding file or files from
> disk. `kill` is an alias for `rm`; it has the same behavior.

## Use these commands remotely

Prefix any command with the target server and token:

```bash
surge --host 192.168.1.10:1700 --token "$SURGE_TOKEN" pause ab12
```

See [remote connections](../../guides/connect-to-a-remote-server.md) for TLS
and token handling.

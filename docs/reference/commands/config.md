# `surge config`

Inspect, search, change, or reset Surge settings without manually editing
`settings.toml`.

## List and search settings

Run the command without arguments to list available settings:

```bash
surge config
```

Pass words to narrow the output:

```bash
surge config network
surge config timeout
```

## Read one setting

Setting paths are case-insensitive. The examples use lowercase category and key
names:

```bash
surge config network.max_concurrent_downloads
```

The command prints the current value. Find valid paths in the configuration
output or in the [configuration reference](../../SETTINGS.md).

## Set a value

Provide a path and value:

```bash
surge config network.max_concurrent_downloads 4
surge config general.auto_resume true
```

Surge validates values before saving. Use the TUI or the settings reference to
understand the unit and safe range for the setting you are changing.

## Restore a default

Replace the value with `default`:

```bash
surge config performance.stall_timeout default
```

## Open the configuration file

```bash
surge config open
```

Surge uses `$EDITOR` when it is set. Otherwise it opens the file with the
platform's default editor. See [Configuration file](../../SETTINGS.md#configuration-file)
for locations and the TOML format.

## Reset everything

`surge --reset-settings` resets settings and keybindings when the application
starts. It is broader than resetting one setting with `surge config`; use it
only when you want to restore the full default configuration.

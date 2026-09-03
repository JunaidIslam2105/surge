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

Use its case-sensitive path:

```bash
surge config Network.Max_Concurrent_Downloads
```

The command prints the current value. Find valid paths in the configuration
output or in the [configuration reference](../../SETTINGS.md).

## Set a value

Provide a path and value:

```bash
surge config Network.Max_Concurrent_Downloads 4
surge config General.Auto_Resume true
```

Surge validates values before saving. Use the TUI or the settings reference to
understand the unit and safe range for the setting you are changing.

## Restore a default

Replace the value with `default`:

```bash
surge config Performance.Stall_Timeout default
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

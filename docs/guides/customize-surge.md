# Customize Surge

Surge can be customized from the TUI or by editing files in its configuration
directory.

## Change settings

Open the settings interface in the TUI, or use the `config` command:

```bash
surge config
surge config General.Auto_Resume true
surge config Performance.Stall_Timeout default
```

`surge config open` opens the settings file in your configured editor. See the
[configuration reference](../SETTINGS.md) for valid paths, values, defaults,
and file locations.

## Change keybindings

Surge generates `keymap.json` on first start. Edit that file to change TUI
shortcuts. Invalid or missing bindings fall back to the built-in defaults.

See [Keymap configuration](../SETTINGS.md#keymap-configuration) for the file
format and platform-specific path.

## Install a font

Surge bundles JetBrains Mono Nerd Font Mono, but the terminal emulator chooses
which font to display. Download `fonts.zip` from the latest release, install the
font on your operating system, then select `JetBrainsMono Nerd Font Mono` in
your terminal settings.

See [Font installation](../FONTS.md) for platform-specific steps.

## Create or install a theme

Place a `.toml` theme file in Surge's themes directory, or pass a path available
to the application. Theme lookup checks a direct path/current directory, a local
`themes/` directory, and then the global themes directory.

See [Custom themes](../THEMES.md) for the file format, palette mapping, and
adaptive light/dark themes.

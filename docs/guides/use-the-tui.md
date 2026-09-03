# Use the Surge TUI

Run `surge` with no arguments to open the interactive dashboard.

```bash
surge
```

The dashboard is the best place to monitor progress, inspect the queue, and
change settings without editing configuration files.

## Add a download

On the dashboard:

- Press `a` to type or paste a URL.
- Press `Shift+A` to import a browser **Copy as cURL** command. Surge extracts
  the URL and request headers such as cookies and the user agent.

Treat exported cURL commands as sensitive. They can contain session cookies or
other credentials; do not paste them into issues or public chats.

## Find shortcuts

Press `h` to show or hide the keyboard-shortcuts overlay. Keybindings can be
customized in `keymap.json`; see [Customize Surge](customize-surge.md).

Press `?` to open the bug-report flow.

## Start without an API server

The TUI starts a local HTTP API server by default so that CLI commands and the
browser extension can target it. If you want an entirely local session, start
it with:

```bash
surge --no-server
```

In this mode, commands such as `surge add`, `surge pause`, and browser-extension
requests cannot control that TUI instance.

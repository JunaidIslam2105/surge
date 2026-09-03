# Tokens, shell completion, and bug reports

## `surge token`

Print the authentication token for a running local Surge server:

```bash
surge token
```

Use this token to connect a remote TUI or configure an API client. Do not paste
it into issues, chat messages, shell history, or repositories.

The system service has a separate token. Retrieve it with:

```bash
surge service token
```

On systems that protect service data, the latter command can require an elevated
terminal.

## `surge completion`

Generate completion code for Bash, Zsh, Fish, or PowerShell:

```bash
surge completion bash
surge completion zsh
surge completion fish
surge completion powershell
```

The command writes the script to standard output. Redirect it to a file or use
your shell's documented completion-installation method. For a temporary Bash
session, for example:

```bash
source <(surge completion bash)
```

## `surge bug-report`

Open Surge's guided GitHub issue flow:

```bash
surge bug-report
```

Before filing, collect the Surge version (`surge --version`), operating system,
the command or steps that reproduce the issue, and relevant logs. Redact tokens,
cookies, private URLs, and file paths before sharing them.

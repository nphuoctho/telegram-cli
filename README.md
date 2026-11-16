# telegram-cli

Command-line Telegram client for a personal account, written in Go with
[cobra](https://github.com/spf13/cobra) and [gotd/td](https://github.com/gotd/td).

```
go install github.com/nphuoctho/telegram-cli@latest
```

## Design docs

- [docs/spec.md](docs/spec.md): what the CLI does: commands, flags, output and JSON
  shapes, exit codes, config and secret files, security rules, and package layout.
- [docs/plan.md](docs/plan.md): the build order in six phases, each with acceptance
  criteria and the tests it must ship.

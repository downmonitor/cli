# Contributing

Thanks for your interest in improving the Downmonitor CLI. Bug reports, fixes
and new features are all welcome.

## Reporting a bug

Open an [issue](https://github.com/DownMonitor/cli/issues) with the command you
ran, what you expected and what happened. `downmonitor --version` and the output
of the command with `--verbose` help a lot (it never prints credentials, but
check anyway before pasting).

For security issues, please follow [SECURITY.md](SECURITY.md) instead.

## Development

You need Go (see `go.mod` for the minimum version) and, for linting,
[golangci-lint](https://golangci-lint.run) v2.

```sh
make build        # ./dist/downmonitor
make test         # go test -race ./...
make lint         # golangci-lint run
make fmt          # gofmt + goimports
make test-install # end-to-end test of install.sh (needs shellcheck and python3)
```

To try a build against a local or staging API:

```sh
make run ARGS="check https://example.com --api-url http://localhost:8000/api/v1"
```

### Project layout

```
cmd/downmonitor     entry point: turns the result of a command into an exit code
internal/cli        one file per command (cobra)
internal/api        HTTP client and API types
internal/config     flags, environment, keychain and .downmonitor.yml resolution
internal/keychain   OS credential store (macOS Keychain, Secret Service)
internal/render     output formats: table, json, ndjson, junit, github, markdown
internal/target     infers the kind of check from the target
internal/ui         colors, alignment and spinner
internal/exitcode   exit codes and the error type that carries them
```

A few conventions keep the code base predictable:

- **Only `main` calls `os.Exit`.** Other packages return errors, and
  `exitcode.Of` maps them to an exit code. Exit codes are part of the public
  interface; changing one is a breaking change.
- **The CLI never computes a verdict.** Verdicts, percentiles and assertion
  results come from the API and are passed through unchanged.
- **stdout is for data only.** Progress, warnings and errors go to stderr so
  machine-readable output can always be piped.
- **API errors are classified by their `error` code**, never by their message.

### Tests

Commands are tested end to end through `cli.Execute` against an
`httptest.Server`, with the keychain replaced by an in-memory store. A new flag
or behavior should come with a test asserting the exit code, the output and
the request sent to the API. Renderers are tested from the fixtures in
`internal/cli/testdata`.

## Pull requests

- Keep pull requests focused on a single change.
- Make sure `make lint test` passes.
- Write commit messages in the imperative mood, prefixed with the area they
  touch, e.g. `check: add --retries` or `render: escape annotation data`.
- Update `README.md` when you change user-facing behavior.

By contributing, you agree that your contributions will be licensed under the
[MIT License](LICENSE).

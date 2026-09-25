# downmonitor

[![CI](https://github.com/DownMonitor/cli/actions/workflows/ci.yml/badge.svg)](https://github.com/DownMonitor/cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/DownMonitor/cli)](https://github.com/DownMonitor/cli/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/downmonitor/cli.svg)](https://pkg.go.dev/github.com/downmonitor/cli)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

The command-line client for [Downmonitor](https://downmonitor.io). Check an
endpoint from several regions of the probe network, once, on a loop, or as a
step of a CI pipeline, with exit codes you can script against.

```console
$ downmonitor check https://api.example.com/health

 UP  https://api.example.com/health
     GET · 200 · 3 regions

     region             status   dns    tcp    tls   ttfb  total  verdict
  ●  cdg Paris, FR         200   8ms   11ms   24ms  121ms  138ms  up
  ●  nyc New York, US      200  12ms   74ms   96ms  168ms  201ms  up
  ●  sin Singapore, SG     200  14ms  164ms  203ms  812ms  871ms  slow

  p50 201ms · p95 871ms · 0 redirects
  ✓ tls Let's Encrypt R11 · expires in 61 days · TLSv1.3
  3 checks billed · ck_7qk3xr2m4a1b2c · cached until 09:41 UTC
```

- **One static binary** for macOS, Linux and Windows, with no runtime
  dependency.
- **Meaningful exit codes**: a target that is down (`1`) is never confused with
  a check that could not run (`3`).
- **CI friendly**: JSON, NDJSON, JUnit, GitHub Actions annotations and
  Markdown output; `--fail-open` so a Downmonitor outage never breaks your
  build.
- **Credentials stay in the OS keychain**, never in a dotfile, and are never
  printed.

## Contents

- [Installation](#installation)
- [Getting started](#getting-started)
- [Choosing regions](#choosing-regions)
- [Requests and assertions](#requests-and-assertions)
- [Watching](#watching)
- [Using it in CI](#using-it-in-ci)
- [Exit codes](#exit-codes)
- [Authentication](#authentication)
- [Configuration](#configuration)
- [Contributing](#contributing)

## Installation

**Install script** (macOS and Linux):

```sh
curl -fsSL https://get.downmonitor.io | sh
```

The script downloads the archive for your platform, verifies it against the
release's `checksums.txt` and installs the binary into `/usr/local/bin` (using
`sudo` if needed). Set `DOWNMONITOR_VERSION=v1.2.3` to pin a release and
`DOWNMONITOR_INSTALL_DIR` to choose another directory.

**Homebrew**:

```sh
brew install downmonitor/tap/downmonitor
```

**Docker**:

```sh
docker run --rm -e DOWNMONITOR_API_KEY ghcr.io/downmonitor/cli check https://example.com
```

**Go**:

```sh
go install github.com/downmonitor/cli/cmd/downmonitor@latest
```

Prebuilt binaries for every platform are also available on the
[releases page](https://github.com/DownMonitor/cli/releases). The checksum file
of each release is signed with [cosign](https://github.com/sigstore/cosign).

**Shell completion**:

```sh
downmonitor completion zsh > "${fpath[1]}/_downmonitor"   # zsh
downmonitor completion bash > /etc/bash_completion.d/downmonitor
downmonitor completion fish > ~/.config/fish/completions/downmonitor.fish
```

## Getting started

```sh
downmonitor login                           # sign in from the browser
downmonitor check https://example.com       # check from two regions
downmonitor regions                         # list the regions in service
downmonitor whoami                          # show the account and remaining checks
```

Run `downmonitor <command> --help` for every flag of a command.

## Choosing regions

By default a check runs from **two regions** picked among those with a probe
in service: on two continents when possible, free-plan regions first, and the
same ones from one run to the next so results stay comparable.

```sh
downmonitor check https://example.com --count 4              # four regions
downmonitor check https://example.com --country FR,DE        # picked in France and Germany
downmonitor check https://example.com --continent europe     # picked in Europe
downmonitor check https://example.com -r cdg,nyc             # exactly these regions
downmonitor check https://example.com -r paris,singapore     # cities work too
downmonitor check https://example.com -r all --exclude sin   # every region but one
```

`--regions` names regions explicitly, while `--country`, `--continent` and
`--count` constrain the automatic selection; the two cannot be combined.
`downmonitor regions` lists what is in service and marks the regions a check
without `--regions` currently uses.

The first source that specifies regions wins: flags, then
`$DOWNMONITOR_REGIONS`, then [`.downmonitor.yml`](#configuration).

## Requests and assertions

Request flags follow curl's names, so most curl commands translate directly:

```sh
downmonitor check https://api.example.com/orders \
  -X POST --json '{"sku": "A-1"}' \
  -H 'X-Request-Source: downmonitor' \
  --bearer "$API_TOKEN"
```

| Flag | Description |
|---|---|
| `-X`, `--method` | HTTP method |
| `-H`, `--header` | Request header, repeatable |
| `-d`, `--data` | Request body; `@file` reads a file, `@-` reads stdin |
| `--json` | JSON body; sets `Content-Type` and `Accept`, defaults to `POST` |
| `-u`, `--basic` | Basic auth as `user:password` |
| `--bearer` | Bearer token for the target (default `$DOWNMONITOR_BEARER`) |
| `--resolve` | `host:port:address`, like curl |
| `--no-follow` | Do not follow redirects |
| `--timeout` | Per-region timeout |
| `-4`, `-6` | Force IPv4 or IPv6 |
| `--insecure` | Ignore certificate errors |

Assertions are evaluated by the probes, where the response is:

```sh
downmonitor check https://api.example.com/health \
  --expect-status 2xx \
  --expect-body '"status":"ok"' \
  --expect-json '$.database.connected == true' \
  --expect-header 'Cache-Control: no-store' \
  --max-ttfb 500 \
  --tls-min-days 14
```

`--expect-json` supports `==`, `!=`, `>`, `>=`, `<`, `<=`, `contains`, `exists`
and `not exists`. Values keep their JSON type: `"ok"` is a string, `3` a
number, `true` a boolean.

## Watching

`watch` runs the same check on a loop and prints one line per reading, marking
every region that changes state:

```console
$ downmonitor watch https://example.com -i 15s
Watching https://example.com from regions picked automatically, every 15s · ctrl-c to stop
09:41:02  ● up    cdg 138ms  nyc 201ms  p95 201ms
09:41:17  ● DOWN  cdg 503  nyc 503  p95 212ms  ▸ cdg: up → down · nyc: up → down
09:41:32  ● up    cdg 141ms  nyc 198ms  p95 198ms  ▸ cdg: down → up · nyc: down → up · recovered after 15s
```

It can also wait for a condition, which is handy after a deployment:

```sh
downmonitor watch "$DEPLOY_URL" --until up --timeout-until 5m && ./smoke-tests.sh
```

`--on-change` runs a shell command on every transition, with
`DOWNMONITOR_VERDICT`, `DOWNMONITOR_TRANSITIONS`, `DOWNMONITOR_REGIONS_DOWN`
and `DOWNMONITOR_CHECK` (the full result as JSON) in its environment. Each
reading is billed like a check; the running total is shown while waiting.

## Using it in CI

```yaml
# .github/workflows/deploy.yml
- name: Check the deployment
  env:
    DOWNMONITOR_API_KEY: ${{ secrets.DOWNMONITOR_API_KEY }}
  run: |
    downmonitor check "$DEPLOY_URL" \
      --count 3 --confirm 2 \
      --expect-status 200 --max-p95 800 \
      --fail-open -o github
```

- `--confirm 2` only reports the target down when at least two regions agree.
- `--fail-open` turns "nothing was checked" (exit code 3) into a warning, so an
  outage on our side never fails your pipeline.
- `--retries 2` re-runs a failing check before giving up.
- `--save result.json` keeps the full result; `--baseline last.json
  --max-regression 50%` fails when the p95 regressed by more than 50%.
- `--fail-on` chooses what fails the command: `any-region-down` (default),
  `all-regions-down`, `assertion`, `tls-expiring` or `never`.

Output formats, selected with `-o`:

| Format | Use |
|---|---|
| `table` | Human-readable output (default) |
| `json` | The API response, unchanged |
| `ndjson` | One JSON object per line, for `watch` |
| `junit` | One test case per region and failed assertion |
| `github` | Markdown table, annotations and job summary on GitHub Actions |
| `markdown` | A table for pull request comments |

Only data is written to stdout: progress, warnings and errors go to stderr, so
`-o json | jq` is safe.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Up, and every assertion passed |
| `1` | Down, or an assertion failed |
| `2` | Usage error: unknown flag or region, unreadable file |
| `3` | Nothing was checked: API unreachable, credential rejected, quota exhausted or no probe available. `--fail-open` turns it into `0` |
| `4` | `watch --until` did not succeed within `--timeout-until` |

## Authentication

Downmonitor uses two kinds of credentials:

- a **Checks API key** (`dm_live_…`) for `check` and `watch`;
- an **organisation token** (`dmt_…`) for workspace access.

`downmonitor login` grants both. It uses the OAuth 2.0 device flow
([RFC 8628](https://www.rfc-editor.org/rfc/rfc8628)): the terminal shows a
short code that you confirm in your browser, so no password goes through the
terminal. Credentials are stored in the macOS Keychain or, on Linux, in the
Secret Service (`secret-tool`).

```sh
downmonitor login                      # both credentials, default profile
downmonitor login --profile staging    # keep several logins apart
downmonitor login --no-browser         # print the URL instead of opening it
downmonitor logout
```

Precedence, for each kind of credential: `--key`, then the environment
(`DOWNMONITOR_API_KEY` or `DOWNMONITOR_TOKEN`), then the stored login. In CI
and on Windows, use the environment variables.

## Configuration

Project defaults can be committed in a `.downmonitor.yml` at the root of a
repository. It is found from any subdirectory, and flags always take
precedence. It must never contain credentials.

```yaml
# .downmonitor.yml
countries: [FR, DE]      # or regions: [cdg, fra], or continents: [EU]
count: 3
exclude: [sin]
confirm: 2
timeout: 10s
headers:
  X-Preview-Bypass: "1"
```

Environment variables:

| Variable | Description |
|---|---|
| `DOWNMONITOR_API_KEY` | Checks API key |
| `DOWNMONITOR_TOKEN` | Organisation token |
| `DOWNMONITOR_PROFILE` | Stored login to use (default `default`) |
| `DOWNMONITOR_REGIONS` | Default regions, comma-separated |
| `DOWNMONITOR_BEARER` | Default bearer token sent to the target |
| `DOWNMONITOR_API_URL` | API base URL |
| `NO_COLOR` | Disable colors |

## Limitations

- Only HTTP and HTTPS targets are supported for now. TCP, ping, DNS and TLS
  targets (`host:port`, `ping://`, `dns://`, `tls://`) are recognized but
  rejected until the API supports them.
- On Windows there is no keychain integration yet: use the environment
  variables.

## Contributing

Bug reports and pull requests are welcome. See
[CONTRIBUTING.md](CONTRIBUTING.md) to get started, and
[SECURITY.md](SECURITY.md) to report a vulnerability.

## License

[MIT](LICENSE)

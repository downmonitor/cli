# downmonitor

Command-line client for [Downmonitor](https://downmonitor.io): check an
endpoint from several regions of the probe network.

## Install

```sh
curl -fsSL https://get.downmonitor.io | sh
```

Or with Homebrew: `brew install downmonitor/tap/downmonitor`.

## Usage

```sh
downmonitor login
downmonitor check https://example.com
downmonitor check https://example.com --regions cdg,nyc
downmonitor watch https://example.com -i 15s
downmonitor regions
```

Exit codes: `0` up, `1` down, `2` usage error, `3` nothing was checked, `4`
`watch --until` timed out.

## Development

```sh
make build
make test
make lint
```

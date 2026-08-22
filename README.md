# downmonitor

Command-line client for [Downmonitor](https://downmonitor.io): check an
endpoint from several regions of the probe network.

## Usage

```sh
export DOWNMONITOR_API_KEY=dm_live_…
downmonitor check https://example.com
downmonitor check https://example.com --regions cdg,nyc
```

Exit codes: `0` up, `1` down, `2` usage error, `3` nothing was checked.

## Building

```sh
go build ./cmd/downmonitor
```

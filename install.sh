#!/bin/sh
# Installs the downmonitor CLI:
#
#   curl -fsSL https://get.downmonitor.io | sh
#
# Downloads the release archive for this platform, verifies it against the
# release's checksums.txt and installs the binary.
#
#   DOWNMONITOR_VERSION=v1.2.3      install this release instead of the latest
#   DOWNMONITOR_INSTALL_DIR=~/bin   install directory (default: /usr/local/bin,
#                                   using sudo if needed, else ~/.local/bin)
#   DOWNMONITOR_DOWNLOAD_URL=…      download from a mirror of the release files
#
# The whole script lives in main(), called on the last line, so a partially
# downloaded script never runs.

set -eu

REPO="${DOWNMONITOR_REPO:-DownMonitor/cli}"

say() { printf '%s\n' "$*" >&2; }
fail() { say "downmonitor: $*"; exit 1; }
has() { command -v "$1" >/dev/null 2>&1; }

detect_os() {
    case "$(uname -s)" in
        Linux) echo linux ;;
        Darwin) echo darwin ;;
        MINGW* | MSYS* | CYGWIN*)
            fail "on Windows, download downmonitor_windows_amd64.zip from https://github.com/$REPO/releases/latest" ;;
        *) fail "unsupported system: $(uname -s)" ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64 | amd64) echo amd64 ;;
        aarch64 | arm64) echo arm64 ;;
        *) fail "unsupported architecture: $(uname -m)" ;;
    esac
}

download() { # url destination
    if has curl; then
        curl -fsSL --retry 3 -o "$2" "$1"
    elif has wget; then
        wget -q -O "$2" "$1"
    else
        fail "curl or wget is needed"
    fi
}

sha256() {
    if has sha256sum; then
        sha256sum "$1" | cut -d' ' -f1
    elif has shasum; then
        shasum -a 256 "$1" | cut -d' ' -f1
    else
        fail "sha256sum or shasum is needed to verify the download"
    fi
}

# Prints the directory to install into.
choose_dir() {
    if [ -n "${DOWNMONITOR_INSTALL_DIR:-}" ]; then
        echo "$DOWNMONITOR_INSTALL_DIR"
    elif [ -w /usr/local/bin ] || { [ "$(id -u)" != 0 ] && has sudo; }; then
        echo /usr/local/bin
    else
        echo "$HOME/.local/bin"
    fi
}

main() {
    os=$(detect_os)
    arch=$(detect_arch)
    archive="downmonitor_${os}_${arch}.tar.gz"

    version="${DOWNMONITOR_VERSION:-latest}"
    if [ -n "${DOWNMONITOR_DOWNLOAD_URL:-}" ]; then
        base="${DOWNMONITOR_DOWNLOAD_URL%/}"
    elif [ "$version" = latest ]; then
        base="https://github.com/$REPO/releases/latest/download"
    else
        case "$version" in v*) ;; *) version="v$version" ;; esac
        base="https://github.com/$REPO/releases/download/$version"
    fi

    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT INT TERM

    say "Downloading $archive ($version)…"
    download "$base/$archive" "$tmp/$archive" || fail "could not download $base/$archive"
    download "$base/checksums.txt" "$tmp/checksums.txt" || fail "could not download $base/checksums.txt"

    expected=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
    [ -n "$expected" ] || fail "$archive is not listed in checksums.txt"
    [ "$(sha256 "$tmp/$archive")" = "$expected" ] || fail "checksum mismatch for $archive: not installing it"

    tar -xzf "$tmp/$archive" -C "$tmp" downmonitor

    dir=$(choose_dir)
    sudo=""
    if [ ! -d "$dir" ]; then
        mkdir -p "$dir" 2>/dev/null || sudo="sudo"
    fi
    if [ -z "$sudo" ] && [ ! -w "$dir" ]; then
        sudo="sudo"
    fi
    if [ -n "$sudo" ]; then
        has sudo || fail "cannot write to $dir; set DOWNMONITOR_INSTALL_DIR"
        say "Installing to $dir (sudo may ask for your password)…"
        sudo mkdir -p "$dir"
    fi
    $sudo install -m 0755 "$tmp/downmonitor" "$dir/downmonitor"

    say "Installed $("$dir/downmonitor" --version 2>/dev/null || echo downmonitor) in $dir"
    case ":$PATH:" in
        *":$dir:"*) ;;
        *) say "$dir is not on your PATH: add  export PATH=\"$dir:\$PATH\"  to your shell profile" ;;
    esac
    say "Next: downmonitor login"
}

main "$@"

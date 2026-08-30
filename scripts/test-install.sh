#!/bin/sh
# Tests install.sh end to end against a local copy of a release: an archive
# for this platform, its checksums.txt and a local HTTP server. Then checks that
# a corrupted archive is refused.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'kill "$server" 2>/dev/null || true; rm -rf "$work"' EXIT INT TERM

case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; esac
case "$(uname -m)" in x86_64 | amd64) arch=amd64 ;; *) arch=arm64 ;; esac
archive="downmonitor_${os}_${arch}.tar.gz"

mkdir -p "$work/release" "$work/stage" "$work/bin"
(cd "$root" && go build -ldflags "-X main.version=9.9.9" -o "$work/stage/downmonitor" ./cmd/downmonitor)
cp "$root/README.md" "$root/LICENSE" "$work/stage/"
tar -czf "$work/release/$archive" -C "$work/stage" downmonitor README.md LICENSE
(cd "$work/release" && { sha256sum "$archive" 2>/dev/null || shasum -a 256 "$archive"; } > checksums.txt)

port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
python3 -m http.server "$port" --bind 127.0.0.1 --directory "$work/release" >/dev/null 2>&1 &
server=$!
for _ in 1 2 3 4 5 6 7 8 9 10; do
    curl -fs "http://127.0.0.1:$port/checksums.txt" >/dev/null 2>&1 && break
    sleep 0.3
done

export DOWNMONITOR_DOWNLOAD_URL="http://127.0.0.1:$port"
export DOWNMONITOR_INSTALL_DIR="$work/bin"

# Piped into sh, as documented.
sh < "$root/install.sh"
"$work/bin/downmonitor" --version | grep -q 9.9.9 || { echo "FAIL: installed binary is not the release's"; exit 1; }
echo "ok: installs and verifies"

rm "$work/bin/downmonitor"
printf 'tampered' >> "$work/release/$archive"
if sh < "$root/install.sh" 2>"$work/err"; then
    echo "FAIL: a corrupted archive was installed"; exit 1
fi
grep -q "checksum mismatch" "$work/err" || { echo "FAIL: unexpected error:"; cat "$work/err"; exit 1; }
[ ! -e "$work/bin/downmonitor" ] || { echo "FAIL: binary present after a checksum mismatch"; exit 1; }
echo "ok: refuses a corrupted archive"

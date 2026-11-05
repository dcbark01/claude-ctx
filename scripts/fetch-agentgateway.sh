#!/bin/sh
# Downloads a pinned agentgateway release into .bin/ for `make e2e`,
# verifying the release's published SHA-256.
set -eu
VERSION="${AGENTGATEWAY_VERSION:-v1.7.0}"
DEST="${1:-.bin}"
case "$(uname -s)" in Darwin) os=darwin ;; Linux) os=linux ;; *) echo "unsupported OS" >&2; exit 1 ;; esac
case "$(uname -m)" in arm64|aarch64) arch=arm64 ;; x86_64|amd64) arch=amd64 ;; *) echo "unsupported arch" >&2; exit 1 ;; esac
asset="agentgateway-$os-$arch"
url="https://github.com/agentgateway/agentgateway/releases/download/$VERSION/$asset"
mkdir -p "$DEST"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
curl -fsSL -o "$tmp/$asset" "$url"
curl -fsSL -o "$tmp/$asset.sha256" "$url.sha256"
want="$(awk '{print $1}' "$tmp/$asset.sha256")"
if command -v sha256sum >/dev/null 2>&1; then got="$(sha256sum "$tmp/$asset" | awk '{print $1}')"
else got="$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')"; fi
[ "$want" = "$got" ] || { echo "checksum mismatch for $asset: want $want got $got" >&2; exit 1; }
chmod +x "$tmp/$asset"
mv "$tmp/$asset" "$DEST/agentgateway"
"$DEST/agentgateway" --version | grep '"version"'

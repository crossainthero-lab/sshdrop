#!/bin/sh
set -eu

REPO="${SSHDROP_REPO:-https://github.com/crossainthero-lab/sshdrop}"
VERSION="${SSHDROP_VERSION:-latest}"
BIN_DIR="${SSHDROP_BIN_DIR:-/usr/local/bin}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$os" in
  linux) os="linux" ;;
  darwin) os="darwin" ;;
  *) echo "Unsupported OS: $os" >&2; exit 1 ;;
esac
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) echo "Unsupported architecture: $arch" >&2; exit 1 ;;
esac

api_repo="${REPO#https://github.com/}"
if [ "$VERSION" = "latest" ]; then
  tag="$(curl -fsSL "https://api.github.com/repos/$api_repo/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)"
else
  tag="$VERSION"
fi
version="${tag#v}"
archive="sshdrop_${version}_${os}_${arch}.tar.gz"
base="$REPO/releases/download/$tag"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $archive"
curl -fsSL "$base/$archive" -o "$tmp/$archive"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"

(cd "$tmp" && grep "  $archive\$" checksums.txt | sha256sum -c -)
tar -xzf "$tmp/$archive" -C "$tmp"

if [ ! -w "$BIN_DIR" ]; then
  echo "Installing to $BIN_DIR requires elevated permissions."
  sudo install "$tmp/sshdrop" "$BIN_DIR/sshdrop"
else
  install "$tmp/sshdrop" "$BIN_DIR/sshdrop"
fi

echo "Installed sshdrop to $BIN_DIR/sshdrop"

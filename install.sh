#!/bin/sh
# One-line install for people without a Go toolchain: downloads the release
# archive matching this machine's OS and arch from GitHub releases, verifies
# it against the published checksums, and unpacks the binary into a bin dir
# on no one's PATH but the user's own — no sudo, no shell profile edited.
#
#   curl -fsSL https://raw.githubusercontent.com/writtendev/writ/main/install.sh | sh
#
# `go install github.com/writtendev/writ/cmd/writ@latest` stays the
# documented path for anyone with a Go toolchain already; this is the other
# one, for anyone who doesn't want to install Go just to get one binary.
set -eu

repo="writtendev/writ"
bin_dir="$HOME/.local/bin"

usage() {
  cat <<EOF
Usage: install.sh [--bin-dir DIR]

Installs the writ binary built for this machine's OS and architecture from
the latest GitHub release. Defaults to \$HOME/.local/bin; pass --bin-dir to
choose another directory. Never uses sudo and never edits your PATH.

Piped from curl, flags go after -s --:
  curl -fsSL .../install.sh | sh -s -- --bin-dir DIR
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --bin-dir)
      [ $# -ge 2 ] || { echo "install.sh: --bin-dir needs a directory" >&2; exit 1; }
      bin_dir=$2
      shift 2
      ;;
    --bin-dir=*)
      bin_dir=${1#--bin-dir=}
      shift
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      echo "install.sh: unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  MINGW* | MSYS* | CYGWIN*) os=windows ;;
  *)
    echo "install.sh: unsupported OS $(uname -s) — writ builds for macOS, Linux, and Windows" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *)
    echo "install.sh: unsupported architecture $(uname -m) — writ builds for amd64 and arm64" >&2
    exit 1
    ;;
esac

latest_url=$(curl -fsSL -o /dev/null -w '%{url_effective}' \
  "https://github.com/$repo/releases/latest") || {
  echo "install.sh: could not reach GitHub to resolve the latest release" >&2
  exit 1
}
case "$latest_url" in
  */releases/tag/*) tag=${latest_url##*/} ;;
  *)
    echo "install.sh: no stable release found for $repo — see https://github.com/$repo/releases" >&2
    exit 1
    ;;
esac
version=${tag#v}

if [ "$os" = "windows" ]; then
  archive="writ_${version}_${os}_${arch}.zip"
  binary="writ.exe"
else
  archive="writ_${version}_${os}_${arch}.tar.gz"
  binary="writ"
fi
base_url="https://github.com/$repo/releases/download/$tag"

workdir=$(mktemp -d)
tmp_bin="$bin_dir/.writ.tmp.$$"
trap 'rm -rf "$workdir"; rm -f "$tmp_bin"' EXIT

echo "install.sh: downloading $archive ($tag)"
curl -fsSL -o "$workdir/$archive" "$base_url/$archive" || {
  echo "install.sh: could not download $base_url/$archive" >&2
  exit 1
}
curl -fsSL -o "$workdir/checksums.txt" "$base_url/checksums.txt" || {
  echo "install.sh: could not download $base_url/checksums.txt" >&2
  exit 1
}

checksum_line=$(grep -F "  $archive" "$workdir/checksums.txt") || {
  echo "install.sh: $archive is not listed in checksums.txt" >&2
  exit 1
}

if command -v sha256sum >/dev/null 2>&1; then
  ( cd "$workdir" && printf '%s\n' "$checksum_line" | sha256sum -c - >/dev/null )
elif command -v shasum >/dev/null 2>&1; then
  ( cd "$workdir" && printf '%s\n' "$checksum_line" | shasum -a 256 -c - >/dev/null )
else
  echo "install.sh: need sha256sum or shasum to verify the download" >&2
  exit 1
fi

mkdir -p "$bin_dir"
if [ "$os" = "windows" ]; then
  unzip -o -j "$workdir/$archive" "$binary" -d "$workdir" >/dev/null
  cp "$workdir/$binary" "$tmp_bin"
else
  tar -xzf "$workdir/$archive" -O "$binary" > "$tmp_bin"
fi
chmod 755 "$tmp_bin"
mv "$tmp_bin" "$bin_dir/$binary"

printf 'installed %s to %s\n' "$tag" "$bin_dir/$binary"
case ":$PATH:" in
  *":$bin_dir:"* | *":$bin_dir/:"*)
    printf 'PATH resolves writ to: %s\n' "$(command -v writ || echo "$bin_dir/$binary")"
    ;;
  *)
    # shellcheck disable=SC2016
    printf '%s is not on your PATH — add it, e.g.: export PATH="%s:$PATH"\n' "$bin_dir" "$bin_dir"
    ;;
esac

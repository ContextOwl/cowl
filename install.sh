#!/bin/sh
set -eu

repo="ContextOwl/cowl"

fail() {
	printf 'cowl install: %s\n' "$*" >&2
	exit 1
}

command -v curl >/dev/null 2>&1 || fail "curl is required"

case "$(uname -s)" in
	Linux) os="linux" ;;
	Darwin) os="darwin" ;;
	*) fail "unsupported operating system: $(uname -s)" ;;
esac

case "$(uname -m)" in
	x86_64 | amd64) arch="amd64" ;;
	aarch64 | arm64) arch="arm64" ;;
	*) fail "unsupported architecture: $(uname -m)" ;;
esac

tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/cowl.XXXXXX")"
trap 'rm -rf "$tmpdir"' EXIT HUP INT TERM

version="${COWL_VERSION:-}"
if [ -z "$version" ]; then
	curl -fsSL "https://api.github.com/repos/$repo/releases/latest" -o "$tmpdir/release.json"
	version="$(awk -F '"' '/"tag_name":/ { print $4; exit }' "$tmpdir/release.json")"
	[ -n "$version" ] || fail "could not determine the latest release"
fi

archive="cowl_${version}_${os}_${arch}.tar.gz"
base_url="https://github.com/$repo/releases/download/$version"

curl -fsSL "$base_url/$archive" -o "$tmpdir/$archive"
curl -fsSL "$base_url/SHA256SUMS" -o "$tmpdir/SHA256SUMS"

expected="$(awk -v file="$archive" '$2 == file { print $1; exit }' "$tmpdir/SHA256SUMS")"
[ -n "$expected" ] || fail "checksum for $archive is missing"
if command -v sha256sum >/dev/null 2>&1; then
	actual="$(sha256sum "$tmpdir/$archive" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
	actual="$(shasum -a 256 "$tmpdir/$archive" | awk '{ print $1 }')"
else
	fail "sha256sum or shasum is required"
fi
[ "$actual" = "$expected" ] || fail "checksum verification failed"

tar -xzf "$tmpdir/$archive" -C "$tmpdir"
[ -f "$tmpdir/cowl" ] || fail "archive did not contain cowl"

install_dir="${COWL_INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$install_dir"
install -m 755 "$tmpdir/cowl" "$install_dir/cowl"
"$install_dir/cowl" version

case ":$PATH:" in
	*":$install_dir:"*) ;;
	*) printf 'Add %s to PATH: export PATH="%s:$PATH"\n' "$install_dir" "$install_dir" ;;
esac

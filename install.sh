#!/bin/sh
# harmos installer.
#
# Detects your OS and architecture, downloads the matching release archive from
# GitHub, verifies its sha256 checksum, and installs the harmos binary to a
# directory on your PATH.
#
#   curl -fsSL https://raw.githubusercontent.com/pottom/harmos/main/install.sh | sh
#
# Options (environment variables):
#   HARMOS_VERSION       a release tag to install (default: the latest release)
#   HARMOS_INSTALL_DIR   where to install (default: /usr/local/bin, else ~/.local/bin)
#
# It never runs anything with elevated privileges itself: if the install directory
# is not writable it says so and stops, rather than reaching for sudo behind your
# back. Windows users: download the .zip from the releases page instead.
set -eu

REPO="pottom/harmos"
BIN="harmos"

info() { printf '%s\n' "$*" >&2; }
warn() { printf 'warning: %s\n' "$*" >&2; }
err() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

# --- prerequisites --------------------------------------------------------------

command -v uname >/dev/null 2>&1 || err "uname not found"
command -v tar >/dev/null 2>&1 || err "tar not found"

if command -v curl >/dev/null 2>&1; then
	http_get() { curl -fsSL "$1"; }
	http_download() { curl -fsSL -o "$1" "$2"; }
elif command -v wget >/dev/null 2>&1; then
	http_get() { wget -qO- "$1"; }
	http_download() { wget -qO "$1" "$2"; }
else
	err "need curl or wget"
fi

# sha256: coreutils on Linux, shasum on macOS/BSD.
if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
	err "need sha256sum or shasum"
fi

# --- platform -------------------------------------------------------------------

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
linux | darwin) ;;
mingw* | msys* | cygwin* | windows*)
	err "this script is for macOS/Linux; on Windows download the .zip from https://github.com/${REPO}/releases" ;;
*) err "unsupported OS: $os" ;;
esac

arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) err "unsupported architecture: $arch (harmos ships amd64 and arm64)" ;;
esac

# --- version --------------------------------------------------------------------

tag="${HARMOS_VERSION:-}"
if [ -z "$tag" ]; then
	info "finding the latest release ..."
	tag=$(http_get "https://api.github.com/repos/${REPO}/releases/latest" |
		grep '"tag_name"' | head -1 |
		sed -e 's/.*"tag_name" *: *"//' -e 's/".*//')
	[ -n "$tag" ] || err "could not determine the latest release"
fi
# The archive names carry the version without its leading v (GoReleaser's convention).
ver="${tag#v}"

asset="${BIN}_${ver}_${os}_${arch}.tar.gz"
base="https://github.com/${REPO}/releases/download/${tag}"

# --- download and verify --------------------------------------------------------

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

info "downloading ${asset} (${tag}) ..."
http_download "${tmp}/${asset}" "${base}/${asset}" ||
	err "download failed — is ${tag} a harmos release with a ${os}/${arch} build? ${base}/${asset}"
http_download "${tmp}/sha256sums.txt" "${base}/sha256sums.txt" ||
	err "could not download sha256sums.txt"

info "verifying checksum ..."
want=$(grep " ${asset}\$" "${tmp}/sha256sums.txt" | cut -d' ' -f1)
[ -n "$want" ] || err "${asset} is not listed in sha256sums.txt"
got=$(sha256 "${tmp}/${asset}")
[ "$want" = "$got" ] || err "checksum mismatch for ${asset}: expected ${want}, got ${got}"

# --- install --------------------------------------------------------------------

tar -xzf "${tmp}/${asset}" -C "$tmp" "$BIN" || err "could not extract ${BIN} from the archive"

dir="${HARMOS_INSTALL_DIR:-}"
if [ -z "$dir" ]; then
	if [ -w /usr/local/bin ] 2>/dev/null; then
		dir=/usr/local/bin
	else
		dir="${HOME}/.local/bin"
	fi
fi
mkdir -p "$dir" || err "could not create install directory: ${dir}"
[ -w "$dir" ] || err "install directory is not writable: ${dir} (set HARMOS_INSTALL_DIR, or run somewhere you can write)"

install -m 0755 "${tmp}/${BIN}" "${dir}/${BIN}" 2>/dev/null ||
	{ cp "${tmp}/${BIN}" "${dir}/${BIN}" && chmod 0755 "${dir}/${BIN}"; } ||
	err "could not install to ${dir}"

info "installed ${BIN} ${tag} to ${dir}/${BIN}"

# A binary on PATH is the point of installing it; if the chosen directory is not on
# PATH, say so instead of leaving a command that "isn't found".
case ":${PATH}:" in
*":${dir}:"*) ;;
*) warn "${dir} is not on your PATH — add it, e.g. export PATH=\"${dir}:\$PATH\"" ;;
esac

"${dir}/${BIN}" --version 2>/dev/null || true

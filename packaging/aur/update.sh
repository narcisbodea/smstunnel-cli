#!/bin/sh
# Writes the AUR package smstunnel-cli-bin (PKGBUILD and .SRCINFO) for one release, from its checksums.txt.
#
#   packaging/aur/update.sh <version> <checksums.txt> <output folder>
#   packaging/aur/update.sh 1.0.1 dist/checksums.txt dist/aur/smstunnel-cli-bin
#
# .SRCINFO comes from makepkg --printsrcinfo, as the AUR asks: on Arch Linux directly, elsewhere in the official
# archlinux container (Docker), as the release workflow does.
set -eu

version=${1:?version, like 1.0.1}
checksums=${2:?the release checksums.txt}
out=${3:?output folder}
here=$(cd "$(dirname "$0")" && pwd)

sum() {
  value=$(awk -v f="smstunnel-cli_${version}_$1.tar.gz" '$2 == f { print $1 }' "$checksums")
  if [ -z "$value" ]; then
    echo "no checksum for smstunnel-cli_${version}_$1.tar.gz in $checksums" >&2
    exit 1
  fi
  echo "$value"
}

amd64=$(sum linux_amd64)
arm64=$(sum linux_arm64)
# pkgver may not hold "-": a pre-release such as 1.1.0-rc1 becomes 1.1.0rc1.
pkgver=$(echo "$version" | tr -d '-')
mkdir -p "$out"
sed -e "s|@PKGVER@|$pkgver|" -e "s|@VERSION@|$version|g" -e "s|@TAG@|v$version|" \
  -e "s|@SHA256_LINUX_AMD64@|$amd64|" -e "s|@SHA256_LINUX_ARM64@|$arm64|" \
  "$here/PKGBUILD.in" >"$out/PKGBUILD"
# The AUR asks for a licence of the package's own files: 0BSD, as Arch encourages.
cp "$here/LICENSE" "$out/LICENSE"

if command -v makepkg >/dev/null 2>&1; then
  (cd "$out" && makepkg --printsrcinfo >.SRCINFO)
else
  abs=$(cd "$out" && pwd)
  if command -v cygpath >/dev/null 2>&1; then
    abs=$(cygpath -w "$abs") # Git Bash on Windows: Docker wants D:\... and no path conversion
    export MSYS_NO_PATHCONV=1
  fi
  # makepkg refuses to run as root: an unprivileged user reads a copy, the mounted folder is not changed.
  docker run --rm -v "$abs:/pkg" archlinux:latest sh -c \
    'useradd -m builder && mkdir /tmp/b && cp /pkg/PKGBUILD /tmp/b/ && chown -R builder /tmp/b && su builder -c "cd /tmp/b && makepkg --printsrcinfo"' \
    >"$out/.SRCINFO"
fi
echo "wrote $out/PKGBUILD and $out/.SRCINFO for smstunnel-cli-bin $pkgver"

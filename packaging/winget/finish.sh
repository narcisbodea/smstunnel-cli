#!/bin/sh
# Finishes the winget manifests GoReleaser wrote (dist/winget/manifests/n/NARBOWEB/SMSTunnelCLI/<version>/) for a
# pull request to microsoft/winget-pkgs: the moniker is the command's name, and the files have CRLF line endings
# (the repository's .editorconfig). Then check them on Windows with: winget validate --manifest <that folder>
#
#   packaging/winget/finish.sh dist/winget/manifests/n/NARBOWEB/SMSTunnelCLI/1.0.1
set -eu
dir=${1:?the version folder of the manifests}
for f in "$dir"/*.yaml; do
  sed -e 's/^Moniker: .*/Moniker: smstunnel/' -e 's/\r$//' "$f" | sed -e 's/$/\r/' >"$f.tmp"
  mv "$f.tmp" "$f"
done
ls -l "$dir"

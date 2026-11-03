# Changelog

All notable changes to the `smstunnel` command-line tool (this native version, `github.com/narcisbodea/smstunnel-cli`)
are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/). Version numbers follow the Node.js version (npm `smstunnel`): the same
version number has the same commands, options, output and exit codes.

## [1.0.0] - 2026-11-03

### Added

- A native `smstunnel` for Windows, macOS and Linux (amd64 and arm64), one static binary with no runtime to install,
  built on [smstunnel-go](https://github.com/narcisbodea/smstunnel-go) 1.0.0. It does what `smstunnel` 1.0.0 on npm
  does, character for character: `login`, `logout`, `phones`, `send`, `code`, `status`, `received` (with `--watch`)
  and `usage`, `--json` on every command, `--idempotency-key`, `--api-base` / `SMSTUNNEL_API_BASE`, and the same exit
  codes.
- The key is saved where the Node.js version saves it (the macOS Keychain, Windows Credential Manager, the Linux
  Secret Service, or `config.json`), in the same format, so either version uses a login made with the other.
- Packages: archives with checksums and SBOMs, `.deb` and `.rpm` (`smstunnel-cli`), a Homebrew formula and a Scoop
  manifest in this repository, winget manifests (`NARBOWEB.SMSTunnelCLI`), an AUR package (`smstunnel-cli-bin`), a
  Chocolatey package (`smstunnel-cli`) and a snap (`smstunnel`).

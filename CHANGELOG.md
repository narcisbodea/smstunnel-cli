# Changelog

All notable changes to the `smstunnel` command-line tool (this native version, `github.com/narcisbodea/smstunnel-cli`)
are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/). Version numbers follow the Node.js version (npm `smstunnel`): the same
version number has the same commands, options, output and exit codes. 1.1.0 is the exception: scheduled texts come in
this version first.

## [1.1.0] - 2026-11-15

### Added

- Scheduled texts. `smstunnel send <number> <message…> --at <time>` hands the text to SMSTunnel now, and SMSTunnel sends
  it 1 minute to 30 days later exactly as if it were sent then with the same key (the phone, the country code and the
  plan's daily allowance of that moment). `--at` takes `"2026-11-16 09:00"` (or `2026-11-16T09:00`) in this
  computer's time zone, or RFC 3339 with an offset (`2026-11-16T09:00:00+02:00`, `2026-11-16T07:00:00Z`); the CLI
  prints the time in UTC too and sends it in UTC. `--json` gives `scheduledId`, `sendAt` and the scheduled text;
  `--idempotency-key` works as for any text, `--wait` doesn't go with `--at`, and `code` has no `--at`.
- `smstunnel scheduled` lists the texts this key scheduled, soonest first, with the account's count against the plan's
  cap; `--done` lists the ones sent (with their message id), failed (with the reason) or canceled in the last 30 days;
  `--limit`, `--page` and `--json` as usual. `smstunnel scheduled cancel <id>` cancels one while it waits: one already
  canceled ends with 0, one that already went out or failed with exit 2 (HTTP 409), an unknown id with exit 5.
- Messages for the new refusals: a time too soon or too far ahead, the plan's cap of waiting texts, a phone that isn't
  the account's, and a connection lost or a 5xx after a scheduled text was handed over (check `smstunnel scheduled`).

### Changed

- Built on [smstunnel-go](https://github.com/narcisbodea/smstunnel-go) 1.1.0.
- The Node.js version (still 1.0.0, not on npm yet) has no scheduled texts: for 1.1.0 the two differ in those commands
  only. See [COMPATIBILITY.md](COMPATIBILITY.md).

## [1.0.0] - 2026-11-03

### Added

- A native `smstunnel` for Windows, macOS and Linux (amd64 and arm64), one static binary with no runtime to install,
  built on [smstunnel-go](https://github.com/narcisbodea/smstunnel-go) 1.0.0. It does what `smstunnel` 1.0.0 on npm
  does, character for character: `login`, `logout`, `phones`, `send`, `code`, `status`, `received` (with `--watch`)
  and `usage`, `--json` on every command, `--idempotency-key`, `--api-base` / `SMSTUNNEL_API_BASE`, and the same exit
  codes.
- The key is saved where the Node.js version saves it (the macOS Keychain, Windows Credential Manager, the Linux
  Secret Service, or `config.json`), in the same format, so either version uses a login made with the other.
- Packages: archives with checksums, SBOMs and build provenance, `.deb` and `.rpm` (`smstunnel-cli`), a Homebrew cask
  and a Scoop manifest in this repository, winget manifests (`NARBOWEB.SMSTunnelCLI`), an AUR package
  (`smstunnel-cli-bin`), a Chocolatey package (`smstunnel-cli`) and a snap (`smstunnel`).

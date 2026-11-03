# Publishing `smstunnel` (the native CLI)

One tag publishes the release; a few channels then take a submission by the owner, as their rules ask.

| Channel | Package | Account needed | What happens | Review |
|---|---|---|---|---|
| GitHub Releases | archives (6), `.deb` (2), `.rpm` (2), `checksums.txt`, SBOMs, provenance | GitHub `narcisbodea` | the Release workflow, after your approval | none |
| Go (`go install`) | `github.com/narcisbodea/smstunnel-cli/cmd/smstunnel` | none | the Go proxy fetches the tag | none |
| Homebrew | cask `smstunnel` in this repository (`Casks/`) | none | the Release workflow commits it | none |
| Scoop | `smstunnel` in this repository (`bucket/`) | none | the Release workflow commits it | none |
| winget | `NARBOWEB.SMSTunnelCLI` | GitHub `narcisbodea` + Microsoft CLA | you open one pull request per version to microsoft/winget-pkgs | automated validation, then a moderator: hours to a few days |
| AUR | `smstunnel-cli-bin` | an AUR account with an SSH key | you push PKGBUILD and .SRCINFO | none (users vote and comment) |
| Chocolatey | `smstunnel-cli` | a Chocolatey Community account and its API key | you push the `.nupkg` | automated validator and verifier, then a human moderator for each version of a new package: days, sometimes 1–2 weeks |
| Snap Store | `smstunnel` | an Ubuntu One account (snapcraft.io) | the Snap Store builds from this repository | automatic for strict confinement, usually minutes |

Nothing in the workflows needs a stored secret: they use the repository's own `GITHUB_TOKEN`.

## 0. Before anything: the repository name

This module is `github.com/narcisbodea/smstunnel-cli`, and so is the `repository` field of the Node.js version's
`package.json` (npm `smstunnel`, not published yet on 3 November 2026; `narcisbodea/smstunnel-cli` did not exist on
GitHub). One repository can't hold both at its root. Choose one:

- **A (recommended)**: this repository is `narcisbodea/smstunnel-cli`. The Node.js version moves to
  `narcisbodea/smstunnel-cli-node` (change `repository`, `homepage` and `bugs` in its `package.json` before its first
  npm publish; npm's trusted publishing checks that `repository` matches the publishing repository). When the npm
  package later becomes a thin wrapper over these binaries (section 10), it moves into this repository.
- **B**: the Node.js version keeps the name and this module gets another (`smstunnel-cli-go`): then change the module
  path in `go.mod` and every import, `.goreleaser.yaml`, the workflows, README and the Homebrew/Scoop/winget/AUR/
  Chocolatey/snap files (URLs).

Also: the Node.js version's PUBLISHING.md plans a Homebrew formula `smstunnel` (installed with Node.js) in a separate
tap `narcisbodea/homebrew-tap`. Publish only one of the two Homebrew packages: both install a `smstunnel` command. This
one needs no Node.js and no token for another repository.

## 1. GitHub repository (once)

1. Create the **public** repository `narcisbodea/smstunnel-cli`, without README, licence or .gitignore (they are here).
2. Push:

   ```bash
   git remote add origin https://github.com/narcisbodea/smstunnel-cli.git
   git push -u origin main
   ```

3. **Settings → Environments → New environment** `release`: **Required reviewers** `narcisbodea`; **Deployment
   branches and tags** → "Selected branches and tags" → add the tag rule `v*`.
4. **Settings → Actions → General → Workflow permissions**: leave "Read repository contents" (the workflows ask for
   write access only where they need it).
5. Branch protection: the Release workflow commits `Casks/smstunnel.rb` and `bucket/smstunnel.json` to `main` with
   `GITHUB_TOKEN`. If you protect `main` (a ruleset that requires pull requests), add **GitHub Actions** as a bypass
   actor of that ruleset, or those two commits fail.
6. Wait for **CI** (Actions tab) to pass: tests on Linux, Windows and macOS with Go 1.24 and the current Go, a snapshot
   release with the `.deb` installed and run, the AUR and winget files, and the snap built and run.

## 2. A release (v1.0.0 and every later one)

1. The version is in two places: `var Version = "…"` in `internal/cli/constants.go` and the top entry of
   `CHANGELOG.md`. Keep it equal to the Node.js version's for the same behaviour.
2. Tag and push:

   ```bash
   git tag -a v1.0.0 -m "smstunnel 1.0.0"
   git push origin v1.0.0
   ```

3. **Actions → Release → Review deployments → Approve** (`release` environment). The workflow checks that the tag
   matches the code and the changelog, runs the tests, then GoReleaser:
   - creates the GitHub release with the changelog entry as notes, the archives (`smstunnel-cli_1.0.0_<os>_<arch>`,
     `.tar.gz`, `.zip` for Windows), `smstunnel-cli_1.0.0_amd64.deb`/`_arm64.deb`,
     `smstunnel-cli-1.0.0-1.x86_64.rpm`/`.aarch64.rpm`, `checksums.txt` and an SBOM per archive;
   - commits `Casks/smstunnel.rb` and `bucket/smstunnel.json` to `main`;
   - adds build provenance (`gh attestation verify <file> --repo narcisbodea/smstunnel-cli`);
   - attaches `NARBOWEB.SMSTunnelCLI*.yaml` (winget), `smstunnel-cli-bin.PKGBUILD` and `smstunnel-cli-bin.SRCINFO`
     (AUR), and, from a Windows job, `smstunnel-cli.1.0.0.nupkg` (Chocolatey).
4. Check from an empty folder: `go install github.com/narcisbodea/smstunnel-cli/cmd/smstunnel@v1.0.0` (the proxy fetches
   the tag; `https://proxy.golang.org/github.com/narcisbodea/smstunnel-cli/@v/v1.0.0.info` answers), and
   `smstunnel --version`.
5. Then sections 4 to 8 for winget, the AUR, Chocolatey and snap.

A published version never changes: fix a mistake with a new version.

## 3. Homebrew and Scoop (no account, nothing to do after the release)

```bash
brew tap narcisbodea/smstunnel-cli https://github.com/narcisbodea/smstunnel-cli
brew install --cask smstunnel
```

```powershell
scoop bucket add smstunnel https://github.com/narcisbodea/smstunnel-cli
scoop install smstunnel/smstunnel
```

GoReleaser writes a cask rather than a formula: it deprecated formulas for pre-built binaries (`brews`), and Homebrew
installs casks on macOS and Linux. The binary is not notarized, so the cask removes macOS's quarantine flag after
installing (GoReleaser's documented hook). To notarize later, sign in the release workflow with an Apple Developer ID
(that needs secrets) and drop the hook.

Check on a Mac: `brew install --cask narcisbodea/smstunnel-cli/smstunnel && smstunnel --version`, and
`brew audit --cask --online narcisbodea/smstunnel-cli/smstunnel`. The official homebrew-cask and Scoop's Main/Extras
buckets take only well-known tools; read their contribution rules (including any on AI-written changes) before ever
submitting there.

## 4. winget (microsoft/winget-pkgs)

Their rules (`AGENTS.md`, `CONTRIBUTING.md`, `doc/FirstContribution.md`): one package version per pull request, manifest
files only, the multi-file manifest with schema 1.12.0 headers, a stable version-specific installer URL, `winget
validate` before submitting, the PR template filled in, and the Microsoft CLA signed. Agents may prepare manifests that
follow these rules; you open the pull request yourself.

1. Fork `microsoft/winget-pkgs` on GitHub (once). Don't clone the whole repository: a sparse checkout of the package
   folder is enough.

   ```powershell
   git clone --filter=blob:none --no-checkout --depth 1 https://github.com/narcisbodea/winget-pkgs.git
   cd winget-pkgs
   git sparse-checkout set manifests/n/NARBOWEB
   git checkout master
   git switch -c NARBOWEB.SMSTunnelCLI-1.0.0
   ```

2. Download the three manifests from the release into `manifests/n/NARBOWEB/SMSTunnelCLI/1.0.0/`
   (`NARBOWEB.SMSTunnelCLI.yaml`, `.installer.yaml`, `.locale.en-US.yaml`; already with CRLF and the moniker
   `smstunnel`).
3. Check them:

   ```powershell
   winget validate --manifest manifests/n/NARBOWEB/SMSTunnelCLI/1.0.0
   winget settings --enable LocalManifestFiles     # once, in an administrator terminal
   winget install --manifest manifests/n/NARBOWEB/SMSTunnelCLI/1.0.0
   smstunnel --version                             # in a new terminal
   winget uninstall NARBOWEB.SMSTunnelCLI
   ```

   (Or `Tools\SandboxTest.ps1 manifests\n\NARBOWEB\SMSTunnelCLI\1.0.0` in Windows Sandbox, which they prefer.)
4. Commit, push to your fork, and open the pull request titled `New package: NARBOWEB.SMSTunnelCLI version 1.0.0`,
   filling in their template (CLA, one manifest, validated, tested, schema 1.12). The validation pipeline adds labels;
   answer the bot within a few days or it closes the PR.
5. Later versions: a new folder `.../SMSTunnelCLI/<version>/` from that release, title
   `Update: NARBOWEB.SMSTunnelCLI to <version>`. (Microsoft's `wingetcreate update NARBOWEB.SMSTunnelCLI --version <v>
   --urls <x64 zip> <arm64 zip> --submit` does the same from your account.)

Checked on 3 November 2026 with a snapshot build: `winget validate` succeeded (winget 1.29.380).

## 5. AUR (smstunnel-cli-bin)

The AUR submission guidelines: the package must not be in the official repositories; pre-built binaries take the
`-bin` suffix; `.SRCINFO` comes from `makepkg --printsrcinfo`; the package's own files get a licence (0BSD, as Arch
encourages: `packaging/aur/LICENSE`); automation can't replace checking each update. No rule on AI-written packages.

1. Create an account at <https://aur.archlinux.org/register> and add your SSH public key (My Account → SSH Public Key).
2. First version, on Arch Linux (or in the `archlinux` container):

   ```bash
   git clone ssh://aur@aur.archlinux.org/smstunnel-cli-bin.git && cd smstunnel-cli-bin
   # from the release: smstunnel-cli-bin.PKGBUILD → PKGBUILD, smstunnel-cli-bin.SRCINFO → .SRCINFO;
   # and LICENSE from packaging/aur/LICENSE
   makepkg -si          # builds from the release archive and installs it
   smstunnel --version
   namcap PKGBUILD *.pkg.tar.zst
   git add PKGBUILD .SRCINFO LICENSE && git commit -m "smstunnel-cli-bin 1.0.0" && git push
   ```

   (Or write them yourself: `packaging/aur/update.sh 1.0.0 checksums.txt <folder>`.)
3. Later versions: the same with the new files (`pkgrel=1` again).

Checked with a snapshot: `makepkg` built it, `namcap` gave only the usual warnings for a Go binary (no PIE/RELRO),
`pacman -U` installed `/usr/bin/smstunnel`, and it worked against a local API.

## 6. Chocolatey (smstunnel-cli)

The package carries `tools\smstunnel.exe` (Chocolatey's preferred way for a vendor's own software) with
`tools\LICENSE.txt` and `tools\VERIFICATION.txt`, and needs no install script: Chocolatey makes the `smstunnel` shim.

1. Create an account at <https://community.chocolatey.org/account/Register> (the user name `narcisbodea` is in the
   nuspec's `owners`; change it if you pick another), then **Account → API Keys** for the push key.
2. Download `smstunnel-cli.1.0.0.nupkg` from the release, and push it from any Windows machine with Chocolatey:

   ```powershell
   choco apikey add --source https://push.chocolatey.org/ --key <your key>     # once
   choco push smstunnel-cli.1.0.0.nupkg --source https://push.chocolatey.org/
   ```

3. Moderation: the validator and the verifier (which installs it in a VM) run automatically; fix what they report
   (a new version of the package, same steps). A human moderator approves each version until the package is trusted.
4. Later versions: the same with the new `.nupkg`. (To push from the workflow instead, add the key as a secret of the
   `release` environment and a `choco push` step.)

Checked with a snapshot: `choco pack` (Chocolatey 2.7.5) made the package.

## 7. Snap Store (smstunnel)

`snap/snapcraft.yaml` builds the snap from this repository's source (Go, `CGO_ENABLED=0`), for amd64 and arm64, strict
confinement. CI builds and runs it on every push.

1. Sign in at <https://snapcraft.io> with an Ubuntu One account and register the name: **My snaps → Register a snap
   name** → `smstunnel` (or `snapcraft register smstunnel`).
2. **My snaps → smstunnel → Builds → Set up**: connect the GitHub repository `narcisbodea/smstunnel-cli`. Each push to
   `main` builds the snap on Launchpad and releases it to the `edge` channel.
3. After a release tag, promote the build of that commit: **Releases** → drag it to `stable` (or
   `snapcraft release smstunnel <revision> stable`).
4. Optional: ask for the `password-manager-service` interface to be connected automatically (forum.snapcraft.io,
   category "store-requests"). Until then users run `sudo snap connect smstunnel:password-manager-service` to keep the
   key in their keyring.

A snap keeps its own config folder (`~/snap/smstunnel/…`), so its login is not shared with a `.deb` or npm install.

## 8. apt and dnf repositories (optional)

The `.deb` and `.rpm` are on each release. A signed apt/dnf repository (like the one being prepared for the desktop
app) can serve `smstunnel-cli` too: the package names don't clash
(the desktop app is `smstunnel-desktop`, `/usr/bin/smstunnel-desktop`; its `Provides: smstunnel` is a virtual name
this package doesn't use).

## 9. Later versions, in short

1. Change `var Version` in `internal/cli/constants.go` and add the `CHANGELOG.md` entry (same number as the Node.js
   version when the behaviour is the same); run `go test ./...`, and `node test/parity/parity.mjs` against the Node.js
   version.
2. Commit, `git tag -a v<version> -m "smstunnel <version>"`, `git push origin main v<version>`, approve the deployment.
3. winget pull request (section 4), AUR push (5), Chocolatey push (6), promote the snap (7).

## 10. Later: the npm package `smstunnel` as a thin package over these binaries

For now npm `smstunnel` stays the Node.js implementation. A later version could ship these binaries instead:

- **Packages**: `smstunnel` (the one people install) and six platform packages in an npm organisation `@smstunnel`
  (to create on npmjs.com): `@smstunnel/cli-win32-x64`, `-win32-arm64`, `-darwin-x64`, `-darwin-arm64`, `-linux-x64`,
  `-linux-arm64`. Each holds one binary (`bin/smstunnel` or `bin/smstunnel.exe`, from the release archives) and
  declares `"os"` and `"cpu"`, so npm installs only the matching one. The Go binaries are static, so one Linux package
  serves glibc and musl (Alpine) alike.
- **`smstunnel`**: `optionalDependencies` on the six, at exactly its own version; `"bin": {"smstunnel": "bin/smstunnel.js"}`,
  a launcher of a few lines that resolves `@smstunnel/cli-${process.platform}-${process.arch}/package.json`, runs that
  binary with `child_process.spawnSync(binary, process.argv.slice(2), { stdio: 'inherit' })`, and ends with its exit
  code (re-raising the signal when it was killed by one). No `postinstall`, no download at install time, so it works
  offline, behind proxies and with `--ignore-scripts`.
- **Fallback**: when no platform package is installed (`--omit=optional`, a lockfile made on another system, an
  unsupported system), the launcher runs the current JavaScript implementation, kept in the package (`dist/`, no
  dependencies), so `npx smstunnel` still works everywhere Node.js runs.
- **Release**: the Release workflow publishes the six platform packages, then `smstunnel`, with npm trusted publishing
  (one trusted publisher per package, environment `npm`), after GoReleaser.

**Is it worth it?** Not yet. Today the two implementations behave identically, the Node.js one has no dependencies and
runs wherever Node.js runs, and the thin package would add six packages to set up and publish on every release, plus
the npm quirks of optional dependencies. It becomes worth it when one implementation must stay the single source of
truth, that is when the next feature lands: then make the Go version the reference, turn npm `smstunnel` into the thin
package above (with the JavaScript fallback for unsupported systems), and stop developing the Node.js code.

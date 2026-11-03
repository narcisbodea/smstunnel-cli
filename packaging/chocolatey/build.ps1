<#
  Packs the Chocolatey package smstunnel-cli for one release: tools\smstunnel.exe from the release's Windows
  amd64 archive, tools\LICENSE.txt and tools\VERIFICATION.txt, then choco pack.

    pwsh packaging/chocolatey/build.ps1 -Version 1.0.1 -Zip dist/smstunnel-cli_1.0.1_windows_amd64.zip -OutDir dist

  Chocolatey makes a shim for tools\smstunnel.exe, so "smstunnel" works in any new terminal, and removes it on
  uninstall: the package needs no install script. Pushing it is the owner's step (PUBLISHING.md).
#>
param(
  [Parameter(Mandatory)] [string] $Version,
  [Parameter(Mandatory)] [string] $Zip,
  [Parameter(Mandatory)] [string] $OutDir
)
$ErrorActionPreference = 'Stop'
$here = $PSScriptRoot
$root = Resolve-Path (Join-Path $here '..\..')
$work = Join-Path ([System.IO.Path]::GetTempPath()) "smstunnel-choco-$Version"
if (Test-Path $work) { Remove-Item -Recurse -Force $work }
New-Item -ItemType Directory -Force (Join-Path $work 'tools') | Out-Null

Copy-Item (Join-Path $here 'smstunnel-cli.nuspec') $work
$extract = Join-Path $work 'zip'
Expand-Archive -Path $Zip -DestinationPath $extract
Copy-Item (Join-Path $extract 'smstunnel.exe') (Join-Path $work 'tools\smstunnel.exe')
Copy-Item (Join-Path $root 'LICENSE') (Join-Path $work 'tools\LICENSE.txt')
Remove-Item -Recurse -Force $extract

$zipHash = (Get-FileHash $Zip -Algorithm SHA256).Hash.ToLowerInvariant()
$exeHash = (Get-FileHash (Join-Path $work 'tools\smstunnel.exe') -Algorithm SHA256).Hash.ToLowerInvariant()
(Get-Content (Join-Path $here 'VERIFICATION.txt.in') -Raw).
  Replace('@VERSION@', $Version).Replace('@SHA256_ZIP@', $zipHash).Replace('@SHA256_EXE@', $exeHash) |
  Set-Content (Join-Path $work 'tools\VERIFICATION.txt') -NoNewline

New-Item -ItemType Directory -Force $OutDir | Out-Null
choco pack (Join-Path $work 'smstunnel-cli.nuspec') --version $Version --outputdirectory (Resolve-Path $OutDir)
if ($LASTEXITCODE -ne 0) { throw "choco pack failed ($LASTEXITCODE)" }
Remove-Item -Recurse -Force $work
Get-ChildItem (Join-Path $OutDir "smstunnel-cli.$Version.nupkg")

[CmdletBinding()]
param(
    [string]$Version = "dev",
    [string]$OutputDirectory = "$PSScriptRoot\..\rep\dist"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path "$PSScriptRoot\.."
$source = Join-Path $repoRoot "rep"

New-Item -ItemType Directory -Force $OutputDirectory | Out-Null

$targets = @(
    @{ GOOS = "linux"; GOARCH = "amd64"; Asset = "rep-linux-x64" },
    @{ GOOS = "linux"; GOARCH = "arm64"; Asset = "rep-linux-arm64" },
    @{ GOOS = "windows"; GOARCH = "amd64"; Asset = "rep-windows-x64.exe" },
    @{ GOOS = "windows"; GOARCH = "arm64"; Asset = "rep-windows-arm64.exe" },
    @{ GOOS = "darwin"; GOARCH = "amd64"; Asset = "rep-macos-x64" },
    @{ GOOS = "darwin"; GOARCH = "arm64"; Asset = "rep-macos-arm64" }
)

foreach ($target in $targets) {
    $env:GOOS = $target.GOOS
    $env:GOARCH = $target.GOARCH
    $output = Join-Path $OutputDirectory $target.Asset
    Write-Host "Building $output..."
    & go build -C $source -ldflags "-X github.com/flcl42/notify/rep/internal/version.Version=$Version" -o $output .
    if ($LASTEXITCODE -ne 0) {
        throw "Build failed for $target.Asset"
    }
}

Write-Host "Done. Assets in $OutputDirectory"

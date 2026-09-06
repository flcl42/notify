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
    @{ GOOS = "linux"; GOARCH = "amd64"; Asset = "nfy-linux-x64" },
    @{ GOOS = "linux"; GOARCH = "arm64"; Asset = "nfy-linux-arm64" },
    @{ GOOS = "windows"; GOARCH = "amd64"; Asset = "nfy-windows-x64.exe" },
    @{ GOOS = "windows"; GOARCH = "arm64"; Asset = "nfy-windows-arm64.exe" },
    @{ GOOS = "darwin"; GOARCH = "amd64"; Asset = "nfy-macos-x64" },
    @{ GOOS = "darwin"; GOARCH = "arm64"; Asset = "nfy-macos-arm64" }
)

try {
    foreach ($target in $targets) {
        $env:GOOS = $target.GOOS
        $env:GOARCH = $target.GOARCH
        # CGO_ENABLED=0 keeps the binaries free of any libc dependency, so a Linux
        # build does not inherit the glibc version of the machine that produced it.
        $env:CGO_ENABLED = "0"
        $output = Join-Path $OutputDirectory $target.Asset
        Write-Host "Building $output..."
        & go build -C $source -trimpath -ldflags "-s -w -X github.com/flcl42/notify/rep/internal/version.Version=$Version" -o $output .
        if ($LASTEXITCODE -ne 0) {
            throw "Build failed for $($target.Asset)"
        }
    }

    foreach ($target in @(
        @{ GOARCH = "amd64"; Asset = "notify-server-linux-x64" },
        @{ GOARCH = "arm64"; Asset = "notify-server-linux-arm64" }
    )) {
        $env:GOOS = "linux"
        $env:GOARCH = $target.GOARCH
        $env:CGO_ENABLED = "0"
        $output = Join-Path $OutputDirectory $target.Asset
        Write-Host "Building $output..."
        & go build -C $source -trimpath -ldflags "-s -w -X github.com/flcl42/notify/rep/internal/version.Version=$Version" -o $output ./cmd/notify-server
        if ($LASTEXITCODE -ne 0) {
            throw "Build failed for $($target.Asset)"
        }
    }
}
finally {
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
}

Write-Host "Done. Assets in $OutputDirectory"

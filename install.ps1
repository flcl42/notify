[CmdletBinding()]
param(
    [string]$Repository = "flcl42/notify",
    [string]$InstallDirectory = "C:\Programs",
    [string]$CredentialPath,
    # Optional: also download private-notify-android.apk (checksum-verified)
    # into the current directory. The CLI directory is kept clean.
    [switch]$Apk
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if ($Repository -notmatch '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$') {
    throw "Repository must use OWNER/NAME form."
}

function Get-ReleaseAsset {
    param(
        [Parameter(Mandatory)] [string]$Name,
        [Parameter(Mandatory)] [string]$Destination
    )

    $uri = "https://github.com/$Repository/releases/latest/download/$Name"
    Invoke-WebRequest -UseBasicParsing $uri -OutFile $Destination
}

function Assert-ReleaseAssetHash {
    param(
        [Parameter(Mandatory)] [string]$Path,
        [Parameter(Mandatory)] [string]$AssetName,
        [Parameter(Mandatory)] [string]$ChecksumPath
    )

    $pattern = '^[A-Fa-f0-9]{64}\s+\*?' + [regex]::Escape($AssetName) + '$'
    $line = Get-Content $ChecksumPath | Where-Object { $_ -match $pattern } | Select-Object -First 1
    if (-not $line) {
        throw "No checksum was published for $AssetName."
    }

    $expected = ($line -split '\s+')[0]
    $sha256 = [System.Security.Cryptography.SHA256]::Create()
    try {
        $stream = [System.IO.File]::OpenRead($Path)
        try {
            $actual = [BitConverter]::ToString($sha256.ComputeHash($stream)).Replace('-', '')
        } finally {
            $stream.Dispose()
        }
    } finally {
        $sha256.Dispose()
    }
    if ($actual -ne $expected) {
        throw "Checksum verification failed for $AssetName."
    }
}

$processorArchitecture = if ($env:PROCESSOR_ARCHITEW6432) {
    $env:PROCESSOR_ARCHITEW6432
} else {
    $env:PROCESSOR_ARCHITECTURE
}

$cliAsset = switch ($processorArchitecture.ToUpperInvariant()) {
    'AMD64' { 'nfy-windows-x64.exe' }
    'ARM64' { 'nfy-windows-arm64.exe' }
    default { throw "Unsupported Windows architecture: $processorArchitecture" }
}

$apkAsset = 'private-notify-android.apk'
$temporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ("nfy-" + [guid]::NewGuid())
$temporaryCli = Join-Path $temporaryDirectory $cliAsset
$temporaryApk = Join-Path $temporaryDirectory $apkAsset
$temporaryChecksums = Join-Path $temporaryDirectory 'SHA256SUMS.txt'

New-Item -ItemType Directory -Force $temporaryDirectory | Out-Null
try {
    Get-ReleaseAsset -Name $cliAsset -Destination $temporaryCli
    Get-ReleaseAsset -Name 'SHA256SUMS.txt' -Destination $temporaryChecksums
    Assert-ReleaseAssetHash -Path $temporaryCli -AssetName $cliAsset -ChecksumPath $temporaryChecksums

    if ($Apk) {
        Get-ReleaseAsset -Name $apkAsset -Destination $temporaryApk
        Assert-ReleaseAssetHash -Path $temporaryApk -AssetName $apkAsset -ChecksumPath $temporaryChecksums
    }

    New-Item -ItemType Directory -Force $InstallDirectory | Out-Null
    $nfyConfig = Join-Path $InstallDirectory 'nfy.yaml'
    $legacyConfig = Join-Path $InstallDirectory 'rep.yaml'
    if (-not (Test-Path -LiteralPath $nfyConfig) -and (Test-Path -LiteralPath $legacyConfig)) {
        Copy-Item -LiteralPath $legacyConfig -Destination $nfyConfig
    }
    Copy-Item -Force $temporaryCli (Join-Path $InstallDirectory 'nfy.exe')
    $legacyCli = Join-Path $InstallDirectory 'rep.exe'
    if (Test-Path -LiteralPath $legacyCli) {
        Remove-Item -LiteralPath $legacyCli -Force
    }
    # Older installers stored the APK next to nfy.exe; the CLI directory
    # is now kept clean, so remove any stale copy.
    $staleApk = Join-Path $InstallDirectory $apkAsset
    if (Test-Path -LiteralPath $staleApk) {
        Remove-Item -LiteralPath $staleApk -Force
    }

    if ($Apk) {
        $apkDestination = Join-Path (Get-Location).Path $apkAsset
        Copy-Item -Force $temporaryApk $apkDestination
    }
} finally {
    Remove-Item -LiteralPath $temporaryDirectory -Recurse -Force -ErrorAction SilentlyContinue
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$pathEntries = @($userPath -split ';' | Where-Object { $_ })
if (-not ($pathEntries | Where-Object { $_.TrimEnd('\') -ieq $InstallDirectory.TrimEnd('\') })) {
    [Environment]::SetEnvironmentVariable('Path', (($pathEntries + $InstallDirectory) -join ';'), 'User')
}
if (-not (($env:Path -split ';') | Where-Object { $_.TrimEnd('\') -ieq $InstallDirectory.TrimEnd('\') })) {
    $env:Path += ";$InstallDirectory"
}

$installedCli = Join-Path $InstallDirectory 'nfy.exe'

if ($CredentialPath) {
    $resolvedCredential = (Resolve-Path $CredentialPath).Path
    & $installedCli credential $resolvedCredential
    if ($LASTEXITCODE -ne 0) {
        throw "nfy.exe could not store the Firebase Admin credential path."
    }
}

Write-Host "Installed CLI: $installedCli"
if ($Apk) {
    $apkDestination = Join-Path (Get-Location).Path $apkAsset
    Write-Host "Downloaded APK: $apkDestination"
    Write-Host 'Install it with: adb install -r .\private-notify-android.apk'
} else {
    Write-Host 'Android app is not included. Re-run with -Apk to also download private-notify-android.apk into the current directory, then: adb install -r .\private-notify-android.apk'
}
if (-not $CredentialPath) {
    Write-Host 'Default delivery uses the hosted relay; no local Firebase Admin key is required.'
    Write-Host 'Next: nfy create "Build Alerts"'
    Write-Host 'For direct mode: nfy mode direct; nfy credential "C:\path\to\firebase-admin-service-account.json"'
}

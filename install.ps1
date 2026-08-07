[CmdletBinding()]
param(
    [string]$Repository = "flcl42/notify",
    [string]$InstallDirectory = "C:\Programs",
    [string]$CredentialPath,
    [switch]$SkipAndroid
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
    Invoke-WebRequest $uri -OutFile $Destination
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

function Find-Adb {
    $command = Get-Command adb -CommandType Application -ErrorAction SilentlyContinue
    if ($command) {
        return $command.Source
    }

    $candidates = @(
        (Join-Path $env:LOCALAPPDATA 'Android\Sdk\platform-tools\adb.exe'),
        (Join-Path $env:USERPROFILE 'AppData\Local\Android\Sdk\platform-tools\adb.exe')
    )
    return $candidates | Where-Object { Test-Path $_ } | Select-Object -First 1
}

$processorArchitecture = if ($env:PROCESSOR_ARCHITEW6432) {
    $env:PROCESSOR_ARCHITEW6432
} else {
    $env:PROCESSOR_ARCHITECTURE
}

$cliAsset = switch ($processorArchitecture.ToUpperInvariant()) {
    'AMD64' { 'rep-windows-x64.exe' }
    'ARM64' { 'rep-windows-arm64.exe' }
    default { throw "Unsupported Windows architecture: $processorArchitecture" }
}

$apkAsset = 'private-notify-android.apk'
$temporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ("private-notify-" + [guid]::NewGuid())
$temporaryCli = Join-Path $temporaryDirectory $cliAsset
$temporaryApk = Join-Path $temporaryDirectory $apkAsset
$temporaryChecksums = Join-Path $temporaryDirectory 'SHA256SUMS.txt'

New-Item -ItemType Directory -Force $temporaryDirectory | Out-Null
try {
    Get-ReleaseAsset -Name $cliAsset -Destination $temporaryCli
    Get-ReleaseAsset -Name $apkAsset -Destination $temporaryApk
    Get-ReleaseAsset -Name 'SHA256SUMS.txt' -Destination $temporaryChecksums
    Assert-ReleaseAssetHash -Path $temporaryCli -AssetName $cliAsset -ChecksumPath $temporaryChecksums
    Assert-ReleaseAssetHash -Path $temporaryApk -AssetName $apkAsset -ChecksumPath $temporaryChecksums

    New-Item -ItemType Directory -Force $InstallDirectory | Out-Null
    Copy-Item -Force $temporaryCli (Join-Path $InstallDirectory 'rep.exe')
    Copy-Item -Force $temporaryApk (Join-Path $InstallDirectory $apkAsset)
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

$installedCli = Join-Path $InstallDirectory 'rep.exe'
$installedApk = Join-Path $InstallDirectory $apkAsset

if ($CredentialPath) {
    $resolvedCredential = (Resolve-Path $CredentialPath).Path
    & $installedCli credential $resolvedCredential
    if ($LASTEXITCODE -ne 0) {
        throw "rep.exe could not store the Firebase Admin credential path."
    }
}

if (-not $SkipAndroid) {
    $adb = Find-Adb
    if (-not $adb) {
        Write-Warning "ADB was not found. The APK is ready at $installedApk."
    } else {
        $connectedDevices = @(& $adb devices | Where-Object { $_ -match "\tdevice$" })
        if ($connectedDevices.Count -eq 0) {
            Write-Warning "No authorized Android device is connected. The APK is ready at $installedApk."
        } else {
            $installOutput = & $adb install -r $installedApk 2>&1
            $installExitCode = $LASTEXITCODE
            $installOutput | ForEach-Object { Write-Host $_ }
            if ($installExitCode -ne 0) {
                throw "Android installation failed. A debug-signed existing app must be removed and paired again before the release-signed APK can be installed."
            }
        }
    }
}

Write-Host "Installed CLI: $installedCli"
Write-Host "Android APK:  $installedApk"
if (-not $CredentialPath) {
    Write-Host 'Next: rep credential "C:\path\to\firebase-admin-service-account.json"'
}

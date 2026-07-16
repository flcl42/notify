[CmdletBinding()]
param(
    [string]$Repository = "flcl42/notify",
    [Parameter(Mandatory)] [string]$GoogleServicesJson,
    [string]$KeystorePath,
    [string]$KeyAlias = "private-notify",
    [switch]$GeneratePasswords,
    [string]$RecoveryPath
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if ($Repository -notmatch '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$') {
    throw "Repository must use OWNER/NAME form."
}
if (-not $KeystorePath) {
    $KeystorePath = Join-Path (Split-Path $PSScriptRoot -Parent) '.release-secrets\private-notify-release.jks'
}
if (-not $RecoveryPath) {
    $RecoveryPath = Join-Path (Split-Path $PSScriptRoot -Parent) '.release-secrets\release-signing-recovery.clixml'
}
if ($GeneratePasswords -and ((Test-Path $KeystorePath) -or (Test-Path $RecoveryPath))) {
    throw "Generated signing material already exists; refusing to overwrite it."
}

$googleServicesPath = (Resolve-Path $GoogleServicesJson).Path
$googleServices = Get-Content $googleServicesPath -Raw | ConvertFrom-Json
$androidPackages = @($googleServices.client.client_info.android_client_info.package_name)
if ($androidPackages -notcontains 'dev.privatenotify') {
    throw "google-services.json does not contain the dev.privatenotify Android client."
}

$gh = (Get-Command gh -CommandType Application -ErrorAction Stop).Source
& $gh auth status
if ($LASTEXITCODE -ne 0) {
    throw "GitHub CLI is not authenticated. Run gh auth login first."
}

function ConvertTo-PlainText {
    param([Parameter(Mandatory)] [Security.SecureString]$Value)

    $pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($Value)
    try {
        return [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer)
    } finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer)
    }
}

function New-RandomPassword {
    $bytes = New-Object byte[] 32
    $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $generator.GetBytes($bytes)
    } finally {
        $generator.Dispose()
    }
    return [BitConverter]::ToString($bytes).Replace('-', '').ToLowerInvariant()
}

function Set-GitHubSecret {
    param(
        [Parameter(Mandatory)] [string]$Name,
        [Parameter(Mandatory)] [string]$Value
    )

    $inputPath = [IO.Path]::GetTempFileName()
    try {
        [IO.File]::WriteAllText($inputPath, $Value, [Text.UTF8Encoding]::new($false))
        $process = Start-Process -FilePath $gh `
            -ArgumentList @('secret', 'set', $Name, '--repo', $Repository) `
            -RedirectStandardInput $inputPath `
            -NoNewWindow `
            -Wait `
            -PassThru
        if ($process.ExitCode -ne 0) {
            throw "Could not set GitHub secret $Name."
        }
    } finally {
        Remove-Item -LiteralPath $inputPath -Force -ErrorAction SilentlyContinue
    }
}

$storePassword = if ($GeneratePasswords) {
    New-RandomPassword
} else {
    ConvertTo-PlainText (Read-Host 'Android keystore password' -AsSecureString)
}
$keyPassword = if ($GeneratePasswords) {
    New-RandomPassword
} else {
    ConvertTo-PlainText (Read-Host 'Android key password' -AsSecureString)
}
if (-not $storePassword -or -not $keyPassword) {
    throw "Signing passwords cannot be empty."
}

$keytoolCommand = Get-Command keytool -CommandType Application -ErrorAction SilentlyContinue
$keytool = if ($keytoolCommand) {
    $keytoolCommand.Source
} else {
    'C:\Program Files\Android\Android Studio\jbr\bin\keytool.exe'
}
if (-not (Test-Path $keytool)) {
    throw "keytool was not found. Install a JDK or Android Studio."
}

$keystoreDirectory = Split-Path $KeystorePath -Parent
New-Item -ItemType Directory -Force $keystoreDirectory | Out-Null
$env:PRIVATE_NOTIFY_STORE_PASSWORD = $storePassword
$env:PRIVATE_NOTIFY_KEY_PASSWORD = $keyPassword
try {
    if (-not (Test-Path $KeystorePath)) {
        & $keytool -genkeypair -noprompt `
            -keystore $KeystorePath `
            -storepass:env PRIVATE_NOTIFY_STORE_PASSWORD `
            -keypass:env PRIVATE_NOTIFY_KEY_PASSWORD `
            -alias $KeyAlias `
            -keyalg RSA `
            -keysize 4096 `
            -sigalg SHA256withRSA `
            -storetype JKS `
            -validity 10000 `
            -dname 'CN=Private Notify, OU=Release, O=flcl42'
        if ($LASTEXITCODE -ne 0) {
            throw "Could not create the Android release keystore."
        }
    }

    & $keytool -list `
        -keystore $KeystorePath `
        -storepass:env PRIVATE_NOTIFY_STORE_PASSWORD `
        -alias $KeyAlias | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "The keystore password or key alias is invalid."
    }

    if ($GeneratePasswords) {
        [pscustomobject]@{
            Repository = $Repository
            KeyAlias = $KeyAlias
            StorePassword = ConvertTo-SecureString $storePassword -AsPlainText -Force
            KeyPassword = ConvertTo-SecureString $keyPassword -AsPlainText -Force
            CreatedAt = [DateTimeOffset]::UtcNow
            GoogleServicesSha256 = (Get-FileHash $googleServicesPath -Algorithm SHA256).Hash
        } | Export-Clixml -LiteralPath $RecoveryPath
    }

    $firebaseBase64 = [Convert]::ToBase64String([IO.File]::ReadAllBytes($googleServicesPath))
    $keystoreBase64 = [Convert]::ToBase64String([IO.File]::ReadAllBytes($KeystorePath))
    Set-GitHubSecret -Name 'FIREBASE_GOOGLE_SERVICES_JSON_BASE64' -Value $firebaseBase64
    Set-GitHubSecret -Name 'ANDROID_SIGNING_KEYSTORE_BASE64' -Value $keystoreBase64
    Set-GitHubSecret -Name 'ANDROID_SIGNING_STORE_PASSWORD' -Value $storePassword
    Set-GitHubSecret -Name 'ANDROID_SIGNING_KEY_ALIAS' -Value $KeyAlias
    Set-GitHubSecret -Name 'ANDROID_SIGNING_KEY_PASSWORD' -Value $keyPassword
} finally {
    Remove-Item Env:PRIVATE_NOTIFY_STORE_PASSWORD -ErrorAction SilentlyContinue
    Remove-Item Env:PRIVATE_NOTIFY_KEY_PASSWORD -ErrorAction SilentlyContinue
    $storePassword = $null
    $keyPassword = $null
}

Write-Host "Configured release secrets for $Repository."
Write-Host "Back up this signing keystore securely: $KeystorePath"
if ($GeneratePasswords) {
    Write-Host "DPAPI-encrypted password recovery file: $RecoveryPath"
}
Write-Host "Do not upload the Firebase Admin service-account JSON; configure it locally with rep credential."

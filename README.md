# Private Notify

Private Notify delivers rare, timely Android notifications without keeping a
socket open. A native Android app receives high-priority FCM data messages,
decrypts them locally with ChaCha20-Poly1305, stores them, and displays normal
Android notifications. The standalone `rep` CLI, written in Go, creates QR subscriptions and
sends encrypted messages by title.

The project is self-hosted: the Android APK's Firebase client configuration and
the sender's Firebase Admin service account must belong to the same Firebase
project.

## Install

GitHub releases contain standalone CLI executables and a signed Android APK.
The Windows installer verifies release checksums, installs `rep.exe` and the APK
under `C:\Programs`, adds that directory to the user `PATH`, and uses ADB to
install the app when an authorized Android device is connected.

Windows, PowerShell:

```powershell
$repo='flcl42/notify'; $i=Join-Path $env:TEMP 'private-notify-install.ps1'; Invoke-WebRequest "https://github.com/$repo/releases/latest/download/install.ps1" -OutFile $i; powershell -NoProfile -ExecutionPolicy Bypass -File $i
```

Pass the local Firebase Admin credential during installation when it is already
available:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File $i -CredentialPath "D:\path\to\firebase-admin-service-account.json"
```

Linux, bash, CLI only:

```bash
repo=flcl42/notify; dir="$HOME/.local/bin"; arch="$(uname -m)"; asset=rep-linux-x64; case "$arch" in aarch64|arm64) asset=rep-linux-arm64;; esac; mkdir -p "$dir"; curl -fsSL "https://github.com/$repo/releases/latest/download/$asset" -o "$dir/rep"; chmod +x "$dir/rep"; grep -qxF 'export PATH="$HOME/.local/bin:$PATH"' "$HOME/.bashrc" || echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$HOME/.bashrc"
```

macOS, zsh, CLI only:

```zsh
repo=flcl42/notify; dir="$HOME/.local/bin"; arch="$(uname -m)"; asset=rep-macos-arm64; [ "$arch" = "x86_64" ] && asset=rep-macos-x64; mkdir -p "$dir"; curl -fsSL "https://github.com/$repo/releases/latest/download/$asset" -o "$dir/rep"; chmod +x "$dir/rep"; grep -qxF 'export PATH="$HOME/.local/bin:$PATH"' "$HOME/.zshrc" || echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$HOME/.zshrc"
```

Android APK only:

```powershell
$repo='flcl42/notify'; Invoke-WebRequest "https://github.com/$repo/releases/latest/download/private-notify-android.apk" -OutFile .\private-notify-android.apk; adb install -r .\private-notify-android.apk
```

An existing debug-signed build cannot be updated by a release-signed APK. If
ADB reports a signature mismatch, uninstalling `dev.privatenotify` removes its
local subscriptions and messages; install the release APK and pair it again.

## First Use

Complete [Firebase Setup](#firebase-setup-generate-the-required-files), then
store the local Firebase Admin service-account path:

```powershell
rep credential "D:\path\to\firebase-admin-service-account.json"
```

Create a private key for a notification title and scan the QR with the Android
app or a general QR scanner:

```powershell
rep create "Build Alerts"
```

The QR is an app-specific `dev.privatenotify://pair?...` URL. Android registers
silently after the link is opened. Press any key in `rep create` to stop waiting;
the generated key remains in `rep.yaml`. Use `--replace` to rotate it.

Send later without maintaining a phone connection:

```powershell
rep "Build Alerts" "The build finished."
rep list
```

Packaged builds store `rep.yaml` next to the executable. It contains private
notification keys, push tokens, and the path to the Firebase Admin JSON. Back it
up as sensitive data.

## Firebase Setup: Generate the Required Files

Private Notify needs two JSON files from one Firebase project. They serve very
different purposes:

| File | Used by | Secret | Destination |
| --- | --- | --- | --- |
| `google-services.json` | Android APK | No; Firebase embeds these client identifiers in the APK | `android/app/google-services.json` |
| Firebase Admin service-account JSON | `rep` sender | Yes; it contains a private key | Keep outside the repository and pass its path to `rep credential` |

The Android client and Admin key must have the same Firebase `project_id`.

### 1. Create a Firebase project

1. Open the [Firebase console](https://console.firebase.google.com/).
2. Select **Create a project**, enter a project name, and continue through the
   project wizard. Google Analytics is optional for Private Notify.
3. Wait for provisioning to finish and open the project overview.

These steps follow Firebase's official
[Android project setup](https://firebase.google.com/docs/android/setup).

### 2. Register the Android app

1. In **Project overview**, select **Add app**, then select **Android**.
2. Enter this Android package name exactly:

   ```text
   dev.privatenotify
   ```

3. The nickname is optional. An SHA certificate fingerprint is not required for
   Firebase Cloud Messaging in this app.
4. Select **Register app**.
5. Download `google-services.json`. Keep the filename unchanged and place it at:

   ```text
   android/app/google-services.json
   ```

The repository ignores this file. Firebase documents it as client
configuration containing non-secret identifiers, but the release workflow
still stores it in a GitHub secret to keep project-specific configuration out
of the public source tree.

### 3. Enable FCM HTTP v1

1. In Firebase, open **Project settings** using the gear beside **Project
   overview**.
2. Open **Cloud Messaging**.
3. Under **Firebase Cloud Messaging API (V1)**, confirm that the API is enabled.
   If Firebase shows it as disabled, follow its link to enable the API in the
   Google Cloud API Library, then return to Firebase.

Private Notify uses the OAuth-authenticated HTTP v1 API. It does not use or need
a legacy FCM server key. See Firebase's
[HTTP v1 sending guide](https://firebase.google.com/docs/cloud-messaging/send/v1-api)
and [server requirements](https://firebase.google.com/docs/cloud-messaging/server-environment).

### 4. Generate the Admin private-key JSON

1. In **Project settings**, open **Service accounts**.
2. Select **Firebase Admin SDK**.
3. Select **Generate new private key**, then confirm **Generate key**.
4. Move the downloaded JSON to a secure local directory outside this checkout.
5. Configure its path on every sender machine:

   ```powershell
   rep credential "D:\secure\firebase-admin-service-account.json"
   ```

The Admin JSON authorizes sends to FCM and contains a private key. Never commit
it, upload it as a GitHub Actions secret, put it in `rep.exe`, attach it to a
release, or encode it into a QR. If it is exposed, revoke that service-account
key in Google Cloud IAM and generate a replacement.

### 5. Verify both files use the same project

Run this before building or pairing:

```powershell
$client = Get-Content .\android\app\google-services.json -Raw | ConvertFrom-Json
$admin = Get-Content "D:\secure\firebase-admin-service-account.json" -Raw | ConvertFrom-Json
$client.project_info.project_id
$admin.project_id
```

The two printed project IDs must be identical. A mismatch lets the Android app
register with one Firebase project while `rep` tries to send through another.

### 6. Configure GitHub release secrets

The release needs the Android client config plus a persistent APK signing key.
After the repository exists, run:

```powershell
.\scripts\configure-github-secrets.ps1 `
  -Repository flcl42/notify `
  -GoogleServicesJson .\android\app\google-services.json `
  -GeneratePasswords
```

The helper creates an ignored Android release keystore and uploads the five
build secrets documented in [docs/RELEASING.md](docs/RELEASING.md). Generated
passwords are saved in an ignored, Windows DPAPI-encrypted recovery file. The
helper does not read or upload the Firebase Admin service-account JSON.

The release APK from this repository is tied to the maintainer's Firebase
project. Other users should fork the repository, configure their own Firebase
and Android signing secrets, and publish releases from that fork.

## Build From Source

Prerequisites are Go 1.23, JDK 17, Android SDK 36, and Android platform tools.

Build the CLI binaries for all platforms:

```powershell
# Windows
.\scripts\build-rep.ps1 -Version 0.2.0
```

```bash
# Linux / macOS / WSL
./scripts/build-rep.sh 0.2.0
```

Run the Go tests:

```bash
cd rep
go test ./...
```

Build the Android app:

```powershell
npm run android:build
npm run android:install
```

If `google-services.json` is absent, the source still compiles but Firebase
registration is disabled.

## How It Works

Pairing creates a random 256-bit key and encodes it with the default title and a
short-lived registration URL in the QR. The Android app obtains an FCM token and
returns it to the CLI. For each send, `rep` encrypts the title, body, and metadata
with ChaCha20-Poly1305 and submits only the encrypted envelope and routing fields
to FCM. Android decrypts the envelope before storing or displaying it.

The phone keeps no custom network connection open. Android and Google Play
services manage push wakeups, which is substantially cheaper at idle than a
persistent application socket. Force-stop, missing notification permission, and
aggressive OEM battery restrictions can still delay or block delivery.

The QR and `rep.yaml` are private. Anyone who obtains a pairing key can decrypt
future messages for that subscription until the title is rotated.

## Release

Branch and pull-request workflows test the CLI and Android build. Tags such as
`release/0.1.0` build six standalone CLI assets and a signed APK, verify the APK
signature, generate SHA-256 checksums, and publish a GitHub release. Android
signing and Firebase client configuration use repository secrets; GitHub's
built-in token publishes the release.

## Status

Android is implemented. iOS is not implemented yet; the encrypted envelope is
designed to support a future APNs provider and Notification Service Extension.

## License

MIT.

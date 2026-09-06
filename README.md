# Private Notify

Private Notify delivers rare, timely Android notifications without keeping a
socket open. A native Android app receives high-priority FCM data messages,
decrypts them locally with ChaCha20-Poly1305, stores them, and displays normal
Android notifications. The standalone `nfy` CLI, written in Go, creates QR subscriptions and
sends encrypted messages by title.

`nfy` has two delivery modes. The default `server` mode sends the already
encrypted envelope through the hosted relay at `https://notify.apps.flcl.me`, so
the sender needs no Google credential. `direct` mode keeps the original fully
local sender and uses a Firebase Admin service-account file on the CLI machine.

## Install

GitHub releases contain standalone CLI executables and a signed Android APK.
The Windows installer verifies release checksums, installs `nfy.exe` and the APK
under `C:\Programs`, adds that directory to the user `PATH`, and uses ADB to
install the app when an authorized Android device is connected. It removes the
retired `rep.exe` command after migrating its configuration.

Windows, PowerShell:

```powershell
$repo='flcl42/notify'; $i=Join-Path $env:TEMP 'private-notify-install.ps1'; Invoke-WebRequest "https://github.com/$repo/releases/latest/download/install.ps1" -OutFile $i; powershell -NoProfile -ExecutionPolicy Bypass -File $i
```

Direct-mode users can pass the local Firebase Admin credential during
installation:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File $i -CredentialPath "D:\path\to\firebase-admin-service-account.json"
```

Linux, bash, CLI only:

```bash
repo=flcl42/notify; dir="$HOME/.local/bin"; arch="$(uname -m)"; asset=nfy-linux-x64; case "$arch" in aarch64|arm64) asset=nfy-linux-arm64;; esac; mkdir -p "$dir"; curl -fsSL "https://github.com/$repo/releases/latest/download/$asset" -o "$dir/nfy"; chmod +x "$dir/nfy"; rm -f "$dir/rep"; grep -qxF 'export PATH="$HOME/.local/bin:$PATH"' "$HOME/.bashrc" || echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$HOME/.bashrc"
```

macOS, zsh, CLI only:

```zsh
repo=flcl42/notify; dir="$HOME/.local/bin"; arch="$(uname -m)"; asset=nfy-macos-arm64; [ "$arch" = "x86_64" ] && asset=nfy-macos-x64; mkdir -p "$dir"; curl -fsSL "https://github.com/$repo/releases/latest/download/$asset" -o "$dir/nfy"; chmod +x "$dir/nfy"; rm -f "$dir/rep"; grep -qxF 'export PATH="$HOME/.local/bin:$PATH"' "$HOME/.zshrc" || echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$HOME/.zshrc"
```

Android APK only:

```powershell
$repo='flcl42/notify'; Invoke-WebRequest "https://github.com/$repo/releases/latest/download/private-notify-android.apk" -OutFile .\private-notify-android.apk; adb install -r .\private-notify-android.apk
```

An existing debug-signed build cannot be updated by a release-signed APK. If
ADB reports a signature mismatch, uninstalling `dev.privatenotify` removes its
local subscriptions and messages; install the release APK and pair it again.

## First Use

Create a private key for a notification title and scan the QR with the Android
app or a general QR scanner:

```powershell
nfy create "Build Alerts"
```

In default server mode the QR is a normal
`https://notify.apps.flcl.me/pair#payload=...` URL, so Android's system scanner
can open it directly in the installed `dev.privatenotify` app through a verified
App Link. The HTTPS page also offers an app-opening fallback for browsers that
do not hand off the link automatically. The private payload stays in the URL
fragment, which is not sent in the HTTP request, and is removed from browser
history by the fallback page. Android registers silently after the app opens.
Press any key in `nfy create` to stop waiting; the generated key remains in
`nfy.yaml`. Use `--replace` to rotate it.

Send later without maintaining a phone connection:

```powershell
nfy "Build Alerts" "The build finished."
nfy list
```

Packaged builds store `nfy.yaml` next to the executable. On first use, `nfy`
copies an adjacent legacy `rep.yaml` when `nfy.yaml` does not exist. The file
contains private notification keys, any direct-mode push tokens, delivery-mode
settings, and an optional path to the Firebase Admin JSON. Back it up as sensitive data. The
environment overrides are `NFY_CONFIG`, `NFY_MODE`, `NFY_SERVER_URL`, and
`NFY_FCM_SERVICE_ACCOUNT`; their legacy `REP_*` names remain supported.

## Delivery Modes

Show or change the persisted mode:

```powershell
nfy mode
nfy mode server
nfy mode direct
```

Server mode is the default for new and existing configurations. It pairs the
phone directly with the relay, then signs every relay request with an Ed25519
identity derived from the QR key. The relay stores the signing public key and
FCM routing token, but never the QR key or notification plaintext. The hosted
relay allows at most 100,000 device deliveries in total per UTC day, 1,000 per
QR subscription per UTC day, and 10 distinct QR subscriptions sending from one
source IP per UTC day. The IP-key counter is charged only by requests with FCM
targets; provisioning unused QR keys does not consume it. IPv6 clients are
grouped by `/64`, and stored IP identities are truncated SHA-256 hashes rather
than raw addresses.

The hosted CLI endpoint, scanner launch page, and phone registration callback
all use HTTPS. `nfy` connects directly to the built-in hosted relay instead of
routing it through `HTTP_PROXY` or `HTTPS_PROXY`; custom relay URLs retain normal
environment-proxy behavior. Notification title and body contents remain
end-to-end encrypted through either mode.

Override the relay temporarily or persist another relay:

```powershell
nfy --server-url http://relay.example:17891 "Build Alerts" "Done"
nfy mode server http://relay.example:17891
```

Direct mode sends to FCM from the CLI machine. It requires the Firebase Admin
credential and local/LAN or ADB access during pairing:

```powershell
nfy mode direct
nfy credential "D:\secure\firebase-admin-service-account.json"
nfy create "Build Alerts" --replace
```

`--mode server` and `--mode direct` override the stored mode for one command.

## Firebase Setup: Generate the Required Files

This setup is required when building an Android release, operating a relay, or
using direct mode. Users of the published APK with the default hosted relay do
not need either file. A custom deployment needs two JSON files from one Firebase
project:

| File | Used by | Secret | Destination |
| --- | --- | --- | --- |
| `google-services.json` | Android APK | No; Firebase embeds these client identifiers in the APK | `android/app/google-services.json` |
| Firebase Admin service-account JSON | Relay or direct-mode `nfy` sender | Yes; it contains a private key | Keep outside the repository; pass its path to the server or `nfy credential` |

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
5. For direct mode, configure its path on every sender machine:

   ```powershell
   nfy credential "D:\secure\firebase-admin-service-account.json"
   ```

The Admin JSON authorizes sends to FCM and contains a private key. A relay reads
it only on the server. Never commit it, put it in `nfy.exe`, attach it to a
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
register with one Firebase project while `nfy` tries to send through another.

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
.\scripts\build-nfy.ps1 -Version 0.4.1
```

```bash
# Linux / macOS / WSL
./scripts/build-nfy.sh 0.4.1
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

Build and run the Linux relay:

```bash
cd rep
CGO_ENABLED=0 go build -o notify-server ./cmd/notify-server
./notify-server \
  --listen :17891 \
  --public-url http://your-server:17891 \
  --state /var/lib/private-notify/state.json \
  --fcm-service-account /secure/firebase-admin-service-account.json \
  --ip-subscription-daily-limit 10 \
  --trusted-proxies 127.0.0.0/8,::1/128
```

The defaults enforce 100,000 total and 1,000 per-subscription FCM deliveries,
plus 10 distinct sending subscriptions per source IP, per UTC day. Relay state
persists all counters across restarts. Configure `--trusted-proxies` with only
the CIDRs of reverse proxies that are allowed to supply `X-Forwarded-For`. See
[`deploy/private-notify.service`](deploy/private-notify.service) for a systemd
unit with automatic restart.

## How It Works

Pairing creates a random 256-bit key and encodes it with the default title and a
registration URL in the QR. The Android app obtains an FCM token and returns it
to either the relay or the direct-mode CLI. For each send, `nfy` encrypts the
title, body, and metadata locally with ChaCha20-Poly1305. In server mode it signs
the encrypted request; the relay checks the signature and quotas, then submits
the opaque envelope to FCM. Android decrypts the envelope before storing or
displaying it.

The phone keeps no custom network connection open. Android and Google Play
services manage push wakeups, which is substantially cheaper at idle than a
persistent application socket. Force-stop, missing notification permission, and
aggressive OEM battery restrictions can still delay or block delivery.

The QR and `nfy.yaml` are private. Anyone who obtains a pairing key can decrypt
future messages for that subscription until the title is rotated.

## Release

Branch and pull-request workflows test the CLI and Android build. Tags such as
`release/0.4.1` build six standalone `nfy` CLI assets, two static Linux relay
assets, and a signed APK, verify the APK signature, generate SHA-256 checksums,
and publish a GitHub release. Android
signing and Firebase client configuration use repository secrets; GitHub's
built-in token publishes the release.

## Status

Android is implemented. iOS is not implemented yet; the encrypted envelope is
designed to support a future APNs provider and Notification Service Extension.

## License

MIT.

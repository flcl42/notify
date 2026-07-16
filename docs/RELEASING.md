# Releasing Private Notify

The release workflow follows the same tag-driven model as `flcl42/pr`. It tests
the source on branches and pull requests. A `v*` or `release/*` tag additionally
builds and publishes standalone CLI binaries plus a signed Android APK.

## GitHub Secrets

Configure these repository secrets before pushing the first release tag:

| Secret | Purpose |
| --- | --- |
| `FIREBASE_GOOGLE_SERVICES_JSON_BASE64` | Base64-encoded Android Firebase client config |
| `ANDROID_SIGNING_KEYSTORE_BASE64` | Base64-encoded persistent Android release keystore |
| `ANDROID_SIGNING_STORE_PASSWORD` | Keystore password |
| `ANDROID_SIGNING_KEY_ALIAS` | Signing key alias |
| `ANDROID_SIGNING_KEY_PASSWORD` | Signing key password |

The Firebase Admin service-account JSON is deliberately not a GitHub secret. It
is not needed to build the software and must not be embedded in an executable or
APK. Keep it on each sender machine and configure its path with `rep credential`.

The Firebase client file is embedded in every Android APK and is therefore not a
server secret. It is stored as a GitHub secret only to keep project-specific
configuration out of the public source tree.

## Configure Secrets

The helper validates the Android package in `google-services.json`, creates a
release keystore when one does not exist, and uploads all five repository
secrets without putting password values in the command line:

```powershell
.\scripts\configure-github-secrets.ps1 `
  -Repository flcl42/notify `
  -GoogleServicesJson .\android\app\google-services.json
```

For unattended initial setup, generate strong random signing passwords and save
them in a Windows DPAPI-encrypted recovery file:

```powershell
.\scripts\configure-github-secrets.ps1 `
  -Repository flcl42/notify `
  -GoogleServicesJson .\android\app\google-services.json `
  -GeneratePasswords
```

Back up `.release-secrets\private-notify-release.jks` and its passwords outside
the repository. The generated `.clixml` recovery file can only be decrypted by
the same Windows account on the same machine, so it is not a substitute for an
independent secure backup. Losing the keystore prevents future APK releases from
updating an installed release build.

## Publish

The tag version must match `package.json`. Bump it for a later release, or keep
the existing version for the first release:

```powershell
npm version patch --no-git-tag-version
$version = (Get-Content .\package.json -Raw | ConvertFrom-Json).version
git add package.json package-lock.json
git commit -m "release $version"
git tag "release/$version"
git push origin master --tags
```

GitHub automatically provides `GITHUB_TOKEN`; no personal access token is
required by the workflow. A successful tagged run publishes:

- `rep-linux-x64`
- `rep-linux-arm64`
- `rep-windows-x64.exe`
- `rep-windows-arm64.exe`
- `rep-macos-x64`
- `rep-macos-arm64`
- `private-notify-android.apk`
- `install.ps1`
- `SHA256SUMS.txt`

The release job can be rerun safely. Existing release assets are replaced with
the artifacts rebuilt from the tagged commit.

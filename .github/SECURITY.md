# Security Policy

## Reporting a vulnerability

Please use GitHub's private vulnerability reporting for this repository. Do not
open a public issue with exploit details, private notification keys, Firebase
credentials, signing material, device tokens, or decrypted message content.

Include the affected version, impact, reproduction steps, and any suggested
mitigation. Remove real credentials and personal notification data from logs or
screenshots.

## Credential exposure

If a Firebase Admin service-account key is exposed, disable or delete that key
in Google Cloud IAM immediately and generate a replacement. If an Android
release signing key or notification subscription key is exposed, treat it as
compromised and rotate it before publishing or sending further notifications.

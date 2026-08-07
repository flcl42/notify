import { readFile } from 'node:fs/promises';
import { createSign } from 'node:crypto';

const TOKEN_URL = 'https://oauth2.googleapis.com/token';
const FCM_SCOPE = 'https://www.googleapis.com/auth/firebase.messaging';

function base64Url(value) {
  return Buffer.from(value).toString('base64url');
}

function signJwt(serviceAccount) {
  const now = Math.floor(Date.now() / 1000);
  const header = {
    alg: 'RS256',
    typ: 'JWT'
  };
  const claims = {
    iss: serviceAccount.client_email,
    scope: FCM_SCOPE,
    aud: TOKEN_URL,
    iat: now,
    exp: now + 3600
  };
  const input = `${base64Url(JSON.stringify(header))}.${base64Url(JSON.stringify(claims))}`;
  const signer = createSign('RSA-SHA256');
  signer.update(input);
  signer.end();
  return `${input}.${signer.sign(serviceAccount.private_key, 'base64url')}`;
}

async function loadServiceAccount(path) {
  if (!path) {
    throw new Error('FCM send requires --fcm-service-account or GOOGLE_APPLICATION_CREDENTIALS.');
  }
  return JSON.parse(await readFile(path, 'utf8'));
}

async function getAccessToken(serviceAccount, tokenUrl = TOKEN_URL) {
  const assertion = signJwt(serviceAccount);
  const response = await fetch(tokenUrl, {
    method: 'POST',
    headers: {
      'content-type': 'application/x-www-form-urlencoded'
    },
    body: new URLSearchParams({
      grant_type: 'urn:ietf:params:oauth:grant-type:jwt-bearer',
      assertion
    })
  });

  const payload = await response.json().catch(async () => ({ raw: await response.text() }));
  if (!response.ok || !payload.access_token) {
    throw new Error(`Could not get Google access token: ${JSON.stringify(payload)}`);
  }

  return payload.access_token;
}

export async function sendFcmPushNotifications(pushTokens, envelope, options = {}) {
  const fcmTokens = pushTokens.filter((item) => item.provider === 'fcm' && item.token);
  if (!fcmTokens.length) {
    return { sent: 0, responses: [] };
  }

  const serviceAccount = await loadServiceAccount(
    options.serviceAccountPath ?? process.env.GOOGLE_APPLICATION_CREDENTIALS ?? process.env.FCM_SERVICE_ACCOUNT
  );
  const projectId = options.projectId ?? serviceAccount.project_id;
  if (!projectId) {
    throw new Error('FCM project id is missing.');
  }

  const accessToken = await getAccessToken(serviceAccount, options.tokenUrl);
  const endpoint =
    options.url ?? `https://fcm.googleapis.com/v1/projects/${encodeURIComponent(projectId)}/messages:send`;
  const responses = [];

  for (const pushToken of fcmTokens) {
    const response = await fetch(endpoint, {
      method: 'POST',
      headers: {
        authorization: `Bearer ${accessToken}`,
        'content-type': 'application/json'
      },
      body: JSON.stringify({
        message: {
          token: pushToken.token,
          data: {
            encryptedEnvelope: JSON.stringify(envelope),
            subscriptionId: envelope.subscriptionId,
            service: String(options.service ?? 'cli')
          },
          android: {
            priority: 'HIGH',
            ttl: String(options.ttl ?? '3600s')
          }
        }
      })
    });

    const body = await response.json().catch(async () => ({ raw: await response.text() }));
    if (!response.ok) {
      throw new Error(`FCM send failed with HTTP ${response.status}: ${JSON.stringify(body)}`);
    }
    responses.push(body);
  }

  return {
    sent: fcmTokens.length,
    responses
  };
}

import assert from 'node:assert/strict';
import http from 'node:http';
import { generateKeyPairSync, randomBytes, randomUUID } from 'node:crypto';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { bytesToBase64Url, decryptNotification, encryptNotification } from './protocol.js';
import { sendFcmPushNotifications } from './fcm.js';

const { privateKey } = generateKeyPairSync('rsa', { modulusLength: 2048 });
const tempDir = await mkdtemp(path.join(os.tmpdir(), 'private-notify-fcm-'));
const serviceAccountPath = path.join(tempDir, 'service-account.json');
await writeFile(
  serviceAccountPath,
  JSON.stringify({
    type: 'service_account',
    project_id: 'test-project',
    private_key: privateKey.export({ type: 'pkcs8', format: 'pem' }),
    client_email: 'test@test-project.iam.gserviceaccount.com'
  }),
  'utf8'
);

const requests = [];
const server = http.createServer((request, response) => {
  let body = '';
  request.on('data', (chunk) => {
    body += chunk;
  });
  request.on('end', () => {
    requests.push({ url: request.url, body });
    response.writeHead(200, { 'content-type': 'application/json' });
    if (request.url === '/token') {
      response.end(JSON.stringify({ access_token: 'test-token', expires_in: 3600, token_type: 'Bearer' }));
    } else {
      response.end(JSON.stringify({ name: 'projects/test/messages/1' }));
    }
  });
});

await new Promise((resolve, reject) => {
  server.once('error', reject);
  server.listen(0, '127.0.0.1', resolve);
});

const { port } = server.address();
const subscription = {
  id: randomUUID(),
  name: 'fcm-test',
  key: bytesToBase64Url(randomBytes(32)),
  pushTokens: [
    {
      provider: 'fcm',
      token: 'fcm-token',
      platform: 'android',
      registeredAt: new Date().toISOString()
    }
  ],
  createdAt: new Date().toISOString()
};
const envelope = encryptNotification(subscription, {
  id: randomUUID(),
  service: 'test',
  title: 'Native title',
  body: 'Native body',
  data: {},
  createdAt: new Date().toISOString()
});

const result = await sendFcmPushNotifications(subscription.pushTokens, envelope, {
  service: 'test',
  serviceAccountPath,
  tokenUrl: `http://127.0.0.1:${port}/token`,
  url: `http://127.0.0.1:${port}/fcm`
});

assert.equal(result.sent, 1);
assert.equal(requests.length, 2);
const fcmBody = JSON.parse(requests[1].body);
assert.equal(fcmBody.message.token, 'fcm-token');
assert.equal(fcmBody.message.android.priority, 'HIGH');
assert.equal(typeof fcmBody.message.data.encryptedEnvelope, 'string');
const decrypted = decryptNotification(subscription, JSON.parse(fcmBody.message.data.encryptedEnvelope));
assert.equal(decrypted.title, 'Native title');
assert.equal(decrypted.body, 'Native body');

server.close();
await rm(tempDir, { recursive: true, force: true });
console.log('fcm ok');

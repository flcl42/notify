import assert from 'node:assert/strict';
import { randomBytes, randomUUID } from 'node:crypto';
import {
  bytesToBase64Url,
  createPairingUrl,
  decodeJsonPayload,
  decryptNotification,
  encryptNotification
} from './protocol.js';

const subscription = {
  id: randomUUID(),
  name: 'test-phone',
  defaultTitle: 'Test Source',
  key: bytesToBase64Url(randomBytes(32)),
  createdAt: new Date().toISOString()
};

const pushPairingUrl = createPairingUrl(subscription, {
  delivery: 'push',
  registrationUrl: 'http://127.0.0.1:8788/register'
});
assert.ok(pushPairingUrl.startsWith('dev.privatenotify://pair?payload='));
assert.ok(pushPairingUrl.includes('payload='));
const encodedPairingPayload = new URL(pushPairingUrl).searchParams.get('payload');
const decodedPairingPayload = decodeJsonPayload(encodedPairingPayload);
assert.equal(decodedPairingPayload.defaultTitle, 'Test Source');

const envelope = encryptNotification(subscription, {
  id: randomUUID(),
  service: 'test',
  title: undefined,
  body: 'Encrypted world',
  data: { priority: 'normal' },
  createdAt: new Date().toISOString()
});

const decrypted = decryptNotification(subscription, envelope);
assert.equal(decrypted.subscriptionId, subscription.id);
assert.equal(decrypted.service, 'test');
assert.equal(decrypted.title, 'Test Source');
assert.equal(decrypted.body, 'Encrypted world');
assert.deepEqual(decrypted.data, { priority: 'normal' });

console.log('protocol ok');

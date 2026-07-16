import { randomBytes } from 'node:crypto';
import { chacha20poly1305 } from '@noble/ciphers/chacha.js';

const encoder = new TextEncoder();
const decoder = new TextDecoder();
export const PAIRING_SCHEME = 'dev.privatenotify';

export function bytesToBase64Url(bytes) {
  return Buffer.from(bytes).toString('base64url');
}

export function base64UrlToBytes(value) {
  return new Uint8Array(Buffer.from(value, 'base64url'));
}

export function textToBytes(value) {
  return encoder.encode(value);
}

export function bytesToText(bytes) {
  return decoder.decode(bytes);
}

export function encodeJsonPayload(payload) {
  return bytesToBase64Url(textToBytes(JSON.stringify(payload)));
}

export function decodeJsonPayload(payload) {
  return JSON.parse(bytesToText(base64UrlToBytes(payload)));
}

export function createPairingUrl(subscription, options = {}) {
  const payload = {
    v: 1,
    type: 'notify-pairing',
    subscriptionId: subscription.id,
    name: subscription.name,
    defaultTitle: subscription.defaultTitle,
    delivery: 'push',
    registrationUrl: options.registrationUrl,
    key: subscription.key,
    createdAt: subscription.createdAt
  };

  return `${PAIRING_SCHEME}://pair?payload=${encodeJsonPayload(payload)}`;
}

export function encryptNotification(subscription, input) {
  const nonce = randomBytes(12);
  const key = base64UrlToBytes(subscription.key);
  const plaintext = {
    v: 1,
    id: input.id,
    subscriptionId: subscription.id,
    service: input.service,
    title: input.title ?? subscription.defaultTitle ?? 'Notification',
    body: input.body,
    data: input.data ?? {},
    createdAt: input.createdAt
  };
  const cipher = chacha20poly1305(key, nonce);
  const ciphertext = cipher.encrypt(textToBytes(JSON.stringify(plaintext)));

  return {
    type: 'notification',
    v: 1,
    subscriptionId: subscription.id,
    nonce: bytesToBase64Url(nonce),
    ciphertext: bytesToBase64Url(ciphertext)
  };
}

export function decryptNotification(subscription, envelope) {
  const key = base64UrlToBytes(subscription.key);
  const nonce = base64UrlToBytes(envelope.nonce);
  const ciphertext = base64UrlToBytes(envelope.ciphertext);
  const cipher = chacha20poly1305(key, nonce);
  const plaintext = cipher.decrypt(ciphertext);
  return JSON.parse(bytesToText(plaintext));
}

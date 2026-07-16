#!/usr/bin/env node
import { randomBytes, randomUUID } from 'node:crypto';
import { networkInterfaces } from 'node:os';
import process from 'node:process';
import qrcode from 'qrcode-terminal';
import { Command } from 'commander';
import { bytesToBase64Url, createPairingUrl } from './protocol.js';
import { findSubscription, getConfigPath, loadConfig, saveConfig, upsertSubscription } from './store.js';
import { createRegistrationServer } from './registration.js';
import { encryptNotification } from './protocol.js';
import { sendFcmPushNotifications } from './fcm.js';

function getLanAddress() {
  const interfaces = networkInterfaces();
  for (const values of Object.values(interfaces)) {
    for (const item of values ?? []) {
      if (item.family === 'IPv4' && !item.internal) {
        return item.address;
      }
    }
  }
  return '127.0.0.1';
}

async function ensureSubscription(options) {
  const now = new Date().toISOString();
  const subscription = {
    id: randomUUID(),
    name: options.name,
    defaultTitle: options.defaultTitle ?? options.name ?? 'Notification',
    key: bytesToBase64Url(randomBytes(32)),
    delivery: 'push',
    pushTokens: [],
    createdAt: now
  };

  await upsertSubscription(subscription);
  return subscription;
}

async function printPairingQr(subscription, pairingOptions) {
  const pairingUrl = createPairingUrl(subscription, pairingOptions);
  console.log(`Subscription: ${subscription.name}`);
  console.log(`Default title: ${subscription.defaultTitle ?? 'Notification'}`);
  console.log(`ID: ${subscription.id}`);
  if (pairingOptions.registrationUrl) {
    console.log(`Registration: ${pairingOptions.registrationUrl}`);
  }
  console.log('Scan this QR in the mobile app. Treat it like a private key.');
  qrcode.generate(pairingUrl, { small: true });
  console.log(pairingUrl);
}

async function waitForPushRegistration(subscription, options) {
  const port = Number(options.port);
  const host = options.host ?? '0.0.0.0';
  const publicHost = host === '0.0.0.0' || host === '::' ? getLanAddress() : host;
  const registrationUrl = `http://${publicHost}:${port}/register`;
  const server = createRegistrationServer({
    subscriptionId: subscription.id,
    port,
    host
  });

  await server.start();
  await printPairingQr(subscription, {
    delivery: 'push',
    registrationUrl
  });
  console.log('Waiting for the mobile app to register its push token...');

  const timeoutMs = Math.max(1, Number(options.waitSeconds ?? 180)) * 1000;
  const timeout = new Promise((_, reject) => {
    setTimeout(() => reject(new Error('Timed out waiting for mobile push registration.')), timeoutMs);
  });

  try {
    const registeredSubscription = await Promise.race([server.registered, timeout]);
    console.log(
      `Registered ${registeredSubscription.pushTokens.length} push token(s) for ${registeredSubscription.name}.`
    );
    return registeredSubscription;
  } finally {
    server.close();
  }
}

async function waitForAnyKey(message) {
  if (!process.stdin.isTTY) {
    return;
  }

  process.stdout.write(message);
  process.stdin.setRawMode(true);
  process.stdin.resume();
  await new Promise((resolve) => {
    process.stdin.once('data', resolve);
  });
  process.stdin.setRawMode(false);
  process.stdin.pause();
  process.stdout.write('\n');
}

async function sendNotification(options) {
  const config = await loadConfig();
  const subscription = findSubscription(config, options.subscription);
  if (!subscription) {
    throw new Error('No matching subscription. Run `npm run notify -- list` to see configured subscriptions.');
  }

  const id = randomUUID();
  const createdAt = new Date().toISOString();
  const envelope = encryptNotification(subscription, {
    id,
    service: options.service,
    title: options.title ?? subscription.defaultTitle ?? 'Notification',
    body: options.body,
    data: options.data ? JSON.parse(options.data) : {},
    createdAt
  });

  const fcmResult = await sendFcmPushNotifications(subscription.pushTokens ?? [], envelope, {
    service: options.service,
    serviceAccountPath: options.fcmServiceAccount,
    projectId: options.fcmProjectId,
    url: options.fcmEndpoint
  });

  if (fcmResult.sent === 0) {
    throw new Error('No FCM push tokens are registered for this subscription.');
  }

  console.log(`Sent encrypted FCM push to ${subscription.name} (${subscription.id}). Tokens: ${fcmResult.sent}.`);
}

const program = new Command();

program
  .name('notify')
  .description('Pair a mobile app and send end-to-end encrypted local notifications.')
  .version('0.1.0');

program
  .command('pair')
  .description('Create a private subscription QR code and register a phone push token.')
  .option('-n, --name <name>', 'subscription name', 'phone')
  .option('-t, --default-title <title>', 'default notification title for this source')
  .option('-p, --port <port>', 'pairing registration port', '8788')
  .option('--host <host>', 'registration bind host', '0.0.0.0')
  .option('--wait-seconds <seconds>', 'seconds to wait for push registration', '180')
  .option('--send-test-on-key', 'after registration, wait for any key and send a test notification')
  .option('--test-body <body>', 'test notification body', 'Test notification from Private Notify CLI.')
  .option('--service <service>', 'source service name for the optional test notification', 'cli')
  .option('--fcm-endpoint <url>', 'explicit FCM send endpoint for testing')
  .option('--fcm-service-account <path>', 'Google service account JSON for FCM HTTP v1')
  .option('--fcm-project-id <id>', 'Firebase project id; defaults to the service account project_id')
  .action(async (options) => {
    const subscription = await ensureSubscription(options);
    const registeredSubscription = await waitForPushRegistration(subscription, options);
    if (options.sendTestOnKey && registeredSubscription) {
      await waitForAnyKey('Press any key to send a test notification with this CLI...');
      await sendNotification({
        subscription: registeredSubscription.id,
        service: options.service,
        body: options.testBody,
        fcmEndpoint: options.fcmEndpoint,
        fcmServiceAccount: options.fcmServiceAccount,
        fcmProjectId: options.fcmProjectId
      });
    }
  });

program
  .command('qr')
  .description('Print a registration QR code for an existing subscription without starting a server.')
  .option('-s, --subscription <id-or-name>', 'subscription id or name')
  .option('-p, --port <port>', 'pairing registration port')
  .option('--host <host>', 'registration host to place in the QR', '0.0.0.0')
  .action(async (options) => {
    const config = await loadConfig();
    const subscription = findSubscription(config, options.subscription);
    if (!subscription) {
      throw new Error('No matching subscription.');
    }
    const port = Number(options.port ?? config.defaultPairPort);
    const publicHost = options.host === '0.0.0.0' || options.host === '::' ? getLanAddress() : options.host;
    await printPairingQr(subscription, {
      delivery: 'push',
      registrationUrl: `http://${publicHost}:${port}/register`
    });
  });

program
  .command('register')
  .description('Start a short-lived registration server for an existing push subscription.')
  .option('-s, --subscription <id-or-name>', 'subscription id or name')
  .option('-p, --port <port>', 'pairing registration port')
  .option('--host <host>', 'registration bind host', '0.0.0.0')
  .option('--wait-seconds <seconds>', 'seconds to wait for push registration', '180')
  .action(async (options) => {
    const config = await loadConfig();
    const subscription = findSubscription(config, options.subscription);
    if (!subscription) {
      throw new Error('No matching subscription.');
    }

    await waitForPushRegistration(subscription, {
      ...options,
      port: options.port ?? config.defaultPairPort
    });
  });

program
  .command('send')
  .description('Send an encrypted notification through FCM.')
  .option('-t, --title <title>', 'notification title; defaults to the paired source title')
  .option('-b, --body <body>', 'notification body', '')
  .option('--service <service>', 'source service name', 'cli')
  .option('-s, --subscription <id-or-name>', 'subscription id or name')
  .option('--fcm-endpoint <url>', 'explicit FCM send endpoint for testing')
  .option('--fcm-service-account <path>', 'Google service account JSON for FCM HTTP v1')
  .option('--fcm-project-id <id>', 'Firebase project id; defaults to the service account project_id')
  .option('--data <json>', 'additional JSON data object')
  .action(sendNotification);

program
  .command('list')
  .description('List saved subscriptions.')
  .action(async () => {
    const config = await loadConfig();
    console.log(`Config: ${getConfigPath()}`);
    if (!config.subscriptions.length) {
      console.log('No subscriptions yet.');
      return;
    }

    for (const subscription of config.subscriptions) {
      const pushTokenCount = Array.isArray(subscription.pushTokens) ? subscription.pushTokens.length : 0;
      console.log(
        `${subscription.name}\t${subscription.id}\t${subscription.defaultTitle ?? 'Notification'}\t${subscription.delivery ?? 'push'}\tpush tokens ${pushTokenCount}\tcreated ${subscription.createdAt}`
      );
    }
  });

program
  .command('config')
  .description('Print the config file path.')
  .action(() => {
    console.log(getConfigPath());
  });

program.parseAsync(process.argv).catch((error) => {
  console.error(`error: ${error.message}`);
  process.exit(1);
});

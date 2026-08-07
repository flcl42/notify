#!/usr/bin/env node
import { randomBytes, randomUUID } from 'node:crypto';
import { existsSync } from 'node:fs';
import { networkInterfaces } from 'node:os';
import process from 'node:process';
import { execFileSync } from 'node:child_process';
import { createInterface } from 'node:readline/promises';
import qrcode from 'qrcode-terminal';
import { Command } from 'commander';
import { bytesToBase64Url, createPairingUrl, encryptNotification } from './protocol.js';
import { createRegistrationServer } from './registration.js';
import { sendFcmPushNotifications } from './fcm.js';
import {
  addRepPushRegistration,
  findRepSubscriptionByTitle,
  getRepConfigPath,
  loadRepConfig,
  resolveFcmServiceAccount,
  saveRepConfig,
  upsertRepSubscription
} from './rep-store.js';

const REP_VERSION = typeof __REP_VERSION__ === 'string' ? __REP_VERSION__ : '0.1.0';

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

function findAdb(explicitPath) {
  const candidates = [
    explicitPath,
    process.env.ADB,
    process.env.LOCALAPPDATA
      ? `${process.env.LOCALAPPDATA}\\Android\\Sdk\\platform-tools\\adb.exe`
      : null,
    process.env.USERPROFILE
      ? `${process.env.USERPROFILE}\\AppData\\Local\\Android\\Sdk\\platform-tools\\adb.exe`
      : null,
    'adb'
  ].filter(Boolean);

  for (const candidate of candidates) {
    try {
      if (candidate.endsWith('.exe') && !existsSync(candidate)) {
        continue;
      }
      execFileSync(candidate, ['version'], { stdio: 'ignore' });
      return candidate;
    } catch {
      // Try the next candidate.
    }
  }

  return null;
}

function setupAdbReverse(port, adbPath) {
  const adb = findAdb(adbPath);
  if (!adb) {
    return false;
  }

  const devices = execFileSync(adb, ['devices'], { encoding: 'utf8' });
  if (!devices.split(/\r?\n/).some((line) => /\tdevice\b/.test(line))) {
    return false;
  }

  execFileSync(adb, ['reverse', `tcp:${port}`, `tcp:${port}`], { stdio: 'ignore' });
  return true;
}

async function promptForTitle(initialTitle) {
  const title = String(initialTitle ?? '').trim();
  if (title) {
    return title;
  }

  const input = createInterface({
    input: process.stdin,
    output: process.stdout
  });
  try {
    return (await input.question('Notification title: ')).trim();
  } finally {
    input.close();
  }
}

function printPairingQr(subscription, registrationUrl) {
  const pairingUrl = createPairingUrl(subscription, {
    delivery: 'push',
    registrationUrl
  });
  console.log(`Title: ${subscription.title}`);
  console.log(`Config: ${getRepConfigPath()}`);
  console.log(`Registration: ${registrationUrl}`);
  console.log('Scan this QR in the Android app. Treat it like a private key.');
  qrcode.generate(pairingUrl, { small: true });
  console.log(pairingUrl);
}

function createKeypressWaiter() {
  let cleanup = () => {};
  if (!process.stdin.isTTY) {
    return {
      promise: new Promise(() => {}),
      cleanup
    };
  }

  const input = process.stdin;
  const wasRaw = input.isRaw;
  const promise = new Promise((resolve) => {
    const onData = () => {
      cleanup();
      resolve();
    };
    cleanup = () => {
      input.off('data', onData);
      if (typeof input.setRawMode === 'function') {
        input.setRawMode(Boolean(wasRaw));
      }
      input.pause();
    };
    if (typeof input.setRawMode === 'function') {
      input.setRawMode(true);
    }
    input.resume();
    input.once('data', onData);
  });

  return {
    promise,
    cleanup
  };
}

async function createSubscription(titleInput, options) {
  const title = await promptForTitle(titleInput);
  if (!title) {
    throw new Error('Notification title is required.');
  }

  const config = await loadRepConfig();
  const existing = findRepSubscriptionByTitle(config, title);
  if (existing && !options.replace) {
    throw new Error(`Title already exists: ${title}. Use --replace to rotate and re-pair it.`);
  }

  const subscription = {
    id: randomUUID(),
    title,
    name: title,
    defaultTitle: title,
    key: bytesToBase64Url(randomBytes(32)),
    delivery: 'push',
    pushTokens: [],
    createdAt: new Date().toISOString()
  };
  await upsertRepSubscription(subscription, { replace: Boolean(options.replace) });

  const port = Number(options.port ?? config.defaultPairPort ?? 8788);
  let host = options.host ?? '0.0.0.0';
  let publicHost = host === '0.0.0.0' || host === '::' ? getLanAddress() : host;

  if (options.usb) {
    const reversed = setupAdbReverse(port, options.adb);
    if (reversed) {
      host = '127.0.0.1';
      publicHost = '127.0.0.1';
      console.log(`ADB reverse active: tcp:${port} -> tcp:${port}`);
    } else {
      console.log('ADB reverse not available; using LAN registration URL.');
    }
  }

  const registrationUrl = `http://${publicHost}:${port}/register`;
  const server = createRegistrationServer({
    subscriptionId: subscription.id,
    port,
    host,
    onRegister: (registration) => addRepPushRegistration(subscription.id, registration)
  });

  await server.start();
  printPairingQr(subscription, registrationUrl);
  console.log('Waiting for phone push-token registration. Press any key to stop...');

  const timeoutMs = Math.max(1, Number(options.waitSeconds ?? 600)) * 1000;
  let timeoutId;
  const timeout = new Promise((_, reject) => {
    timeoutId = setTimeout(() => reject(new Error('Timed out waiting for mobile push registration.')), timeoutMs);
  });
  const keypress = createKeypressWaiter();

  try {
    const result = await Promise.race([
      server.registered.then((registeredSubscription) => ({
        type: 'registered',
        registeredSubscription
      })),
      timeout,
      keypress.promise.then(() => ({
        type: 'cancelled'
      }))
    ]);
    if (result.type === 'cancelled') {
      console.log('Stopped. The private key remains in rep.yaml; run create --replace to rotate it.');
      return;
    }

    const registeredSubscription = result.registeredSubscription;
    console.log(
      `Registered ${registeredSubscription.pushTokens.length} push token(s) for "${registeredSubscription.title}".`
    );
  } finally {
    clearTimeout(timeoutId);
    keypress.cleanup();
    server.close();
  }
}

function resolveTitleAndBody(config, args) {
  for (let length = args.length - 1; length >= 1; length -= 1) {
    const candidate = args.slice(0, length).join(' ');
    const subscription = findRepSubscriptionByTitle(config, candidate);
    if (subscription) {
      return {
        subscription,
        body: args.slice(length).join(' ')
      };
    }
  }

  return {
    subscription: null,
    body: ''
  };
}

async function sendFromArgs(args, options = {}) {
  if (!args.length) {
    throw new Error('Usage: rep <title> <notification text>');
  }

  const config = await loadRepConfig();
  const { subscription, body } = resolveTitleAndBody(config, args);
  if (!subscription) {
    const titles = config.subscriptions.map((item) => item.title).join(', ') || 'none';
    throw new Error(`No title matched. Known titles: ${titles}`);
  }
  if (!body) {
    throw new Error('Notification text is required after the title.');
  }

  const envelope = encryptNotification(subscription, {
    id: randomUUID(),
    service: options.service ?? 'rep',
    title: subscription.title,
    body,
    data: {},
    createdAt: new Date().toISOString()
  });
  const fcmResult = await sendFcmPushNotifications(subscription.pushTokens ?? [], envelope, {
    service: options.service ?? 'rep',
    serviceAccountPath: resolveFcmServiceAccount(config, options.fcmServiceAccount),
    projectId: options.fcmProjectId
  });

  if (fcmResult.sent === 0) {
    throw new Error(`No FCM push tokens are registered for "${subscription.title}". Run: rep create "${subscription.title}"`);
  }

  console.log(`Sent "${subscription.title}" notification. Tokens: ${fcmResult.sent}.`);
}

async function listSubscriptions() {
  const config = await loadRepConfig();
  console.log(`Config: ${getRepConfigPath()}`);
  if (!config.subscriptions.length) {
    console.log('No titles yet.');
    return;
  }

  for (const subscription of config.subscriptions) {
    const pushTokenCount = Array.isArray(subscription.pushTokens) ? subscription.pushTokens.length : 0;
    console.log(`${subscription.title}\tpush tokens ${pushTokenCount}\tcreated ${subscription.createdAt}`);
  }
}

async function saveServiceAccountPath(serviceAccountPath) {
  if (!serviceAccountPath) {
    throw new Error('Service-account JSON path is required.');
  }
  const config = await loadRepConfig();
  config.fcmServiceAccount = serviceAccountPath;
  await saveRepConfig(config);
  console.log(`Stored FCM credential path in ${getRepConfigPath()}`);
}

const program = new Command();

program
  .name('rep')
  .description('Send encrypted Android notifications by title.')
  .version(REP_VERSION)
  .option('--fcm-service-account <path>', 'Firebase Admin service-account JSON path for this send')
  .option('--fcm-project-id <id>', 'Firebase project id; defaults to the service account project_id')
  .option('--service <service>', 'source service name', 'rep')
  .argument('[args...]', 'title followed by notification text')
  .action((args, options) => sendFromArgs(args, options));

program
  .command('create [title]')
  .description('Create a private notification key for a title, print a QR, and register the phone.')
  .option('-p, --port <port>', 'pairing registration port')
  .option('--host <host>', 'registration bind host when USB reverse is unavailable', '0.0.0.0')
  .option('--wait-seconds <seconds>', 'seconds to wait for phone registration', '600')
  .option('--adb <path>', 'adb executable path')
  .option('--no-usb', 'do not try adb reverse; use LAN registration instead')
  .option('--replace', 'replace an existing title with a new private key')
  .action(createSubscription);

program
  .command('list')
  .description('List configured notification titles.')
  .action(listSubscriptions);

program
  .command('config')
  .description('Print rep.yaml path.')
  .action(() => {
    console.log(getRepConfigPath());
  });

program
  .command('credential <path>')
  .description('Store the Firebase Admin service-account JSON path in rep.yaml.')
  .action(saveServiceAccountPath);

program.parseAsync(process.argv).catch((error) => {
  console.error(`error: ${error.message}`);
  process.exit(1);
});

import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const DEFAULT_CONFIG = {
  v: 1,
  defaultPairPort: 8788,
  subscriptions: []
};

export function getConfigDir() {
  return process.env.NOTIFY_HOME || path.join(os.homedir(), '.private-notify');
}

export function getConfigPath() {
  return path.join(getConfigDir(), 'subscriptions.json');
}

export async function loadConfig() {
  const configPath = getConfigPath();
  if (!existsSync(configPath)) {
    return { ...DEFAULT_CONFIG, subscriptions: [] };
  }

  const raw = await readFile(configPath, 'utf8');
  const parsed = JSON.parse(raw);
  return {
    ...DEFAULT_CONFIG,
    ...parsed,
    subscriptions: Array.isArray(parsed.subscriptions) ? parsed.subscriptions : []
  };
}

export async function saveConfig(config) {
  await mkdir(getConfigDir(), { recursive: true });
  await writeFile(getConfigPath(), `${JSON.stringify(config, null, 2)}\n`, 'utf8');
}

export async function upsertSubscription(subscription) {
  const config = await loadConfig();
  const existingIndex = config.subscriptions.findIndex((item) => item.id === subscription.id);

  if (existingIndex >= 0) {
    config.subscriptions[existingIndex] = {
      ...config.subscriptions[existingIndex],
      ...subscription
    };
  } else {
    config.subscriptions.push(subscription);
  }

  await saveConfig(config);
  return config;
}

export async function addPushRegistration(subscriptionId, registration) {
  const config = await loadConfig();
  const subscription = config.subscriptions.find((item) => item.id === subscriptionId);

  if (!subscription) {
    throw new Error('Unknown subscription.');
  }

  const pushTokens = Array.isArray(subscription.pushTokens) ? subscription.pushTokens : [];
  const tokenValue = registration.token;
  const nextToken = {
    provider: registration.provider ?? 'fcm',
    token: tokenValue,
    platform: registration.platform ?? 'unknown',
    registeredAt: registration.registeredAt ?? new Date().toISOString()
  };
  const existingIndex = pushTokens.findIndex((item) => item.token === tokenValue);
  const nextPushTokens =
    existingIndex >= 0
      ? pushTokens.map((item) => (item.token === tokenValue ? { ...item, ...nextToken } : item))
      : [nextToken, ...pushTokens];

  subscription.delivery = 'push';
  subscription.pushTokens = nextPushTokens;
  await saveConfig(config);

  return { config, subscription };
}

export function findSubscription(config, idOrName) {
  if (!config.subscriptions.length) {
    return null;
  }

  if (!idOrName) {
    return config.subscriptions[0];
  }

  return config.subscriptions.find(
    (subscription) => subscription.id === idOrName || subscription.name === idOrName
  ) ?? null;
}

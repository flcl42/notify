import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import path from 'node:path';
import YAML from 'yaml';

const DEFAULT_CONFIG = {
  v: 1,
  defaultPairPort: 8788,
  subscriptions: []
};

const INSTALLED_CONFIG_PATH = 'C:\\Programs\\rep.yaml';

export function getRepConfigPath() {
  if (process.env.REP_CONFIG) {
    return process.env.REP_CONFIG;
  }

  if (process.pkg || path.basename(process.execPath).toLowerCase() === 'rep.exe') {
    return path.join(path.dirname(process.execPath), 'rep.yaml');
  }

  return INSTALLED_CONFIG_PATH;
}

function normalizeSubscription(subscription) {
  const title = String(subscription.title ?? subscription.defaultTitle ?? subscription.name ?? '').trim();
  return {
    ...subscription,
    title,
    name: subscription.name ?? title,
    defaultTitle: subscription.defaultTitle ?? title,
    pushTokens: Array.isArray(subscription.pushTokens) ? subscription.pushTokens : []
  };
}

export async function loadRepConfig() {
  const configPath = getRepConfigPath();
  if (!existsSync(configPath)) {
    return {
      ...DEFAULT_CONFIG,
      subscriptions: []
    };
  }

  const raw = await readFile(configPath, 'utf8');
  const parsed = YAML.parse(raw) ?? {};
  return {
    ...DEFAULT_CONFIG,
    ...parsed,
    subscriptions: Array.isArray(parsed.subscriptions)
      ? parsed.subscriptions.map(normalizeSubscription).filter((item) => item.title)
      : []
  };
}

export async function saveRepConfig(config) {
  const configPath = getRepConfigPath();
  await mkdir(path.dirname(configPath), { recursive: true });
  const normalized = {
    ...DEFAULT_CONFIG,
    ...config,
    subscriptions: Array.isArray(config.subscriptions)
      ? config.subscriptions.map(normalizeSubscription).filter((item) => item.title)
      : []
  };

  await writeFile(configPath, YAML.stringify(normalized), 'utf8');
  return normalized;
}

export function findRepSubscriptionByTitle(config, title) {
  const wanted = String(title ?? '').trim().toLowerCase();
  if (!wanted) {
    return null;
  }

  return (
    config.subscriptions.find((subscription) => subscription.title.toLowerCase() === wanted) ??
    null
  );
}

export async function upsertRepSubscription(subscription, options = {}) {
  const config = await loadRepConfig();
  const normalized = normalizeSubscription(subscription);
  const existingIndex = config.subscriptions.findIndex(
    (item) => item.title.toLowerCase() === normalized.title.toLowerCase()
  );

  if (existingIndex >= 0 && !options.replace) {
    throw new Error(`Title already exists in rep.yaml: ${normalized.title}`);
  }

  if (existingIndex >= 0) {
    config.subscriptions[existingIndex] = {
      ...config.subscriptions[existingIndex],
      ...normalized
    };
  } else {
    config.subscriptions.push(normalized);
  }

  return saveRepConfig(config);
}

export async function addRepPushRegistration(subscriptionId, registration) {
  const config = await loadRepConfig();
  const subscription = config.subscriptions.find((item) => item.id === subscriptionId);
  if (!subscription) {
    throw new Error('Unknown subscription.');
  }

  const tokenValue = registration.token;
  const nextToken = {
    provider: registration.provider ?? 'fcm',
    token: tokenValue,
    platform: registration.platform ?? 'unknown',
    registeredAt: registration.registeredAt ?? new Date().toISOString()
  };
  const pushTokens = Array.isArray(subscription.pushTokens) ? subscription.pushTokens : [];
  const existingIndex = pushTokens.findIndex((item) => item.token === tokenValue);
  subscription.pushTokens =
    existingIndex >= 0
      ? pushTokens.map((item) => (item.token === tokenValue ? { ...item, ...nextToken } : item))
      : [nextToken, ...pushTokens];
  subscription.delivery = 'push';

  await saveRepConfig(config);
  return { config, subscription: normalizeSubscription(subscription) };
}

export function resolveFcmServiceAccount(config, explicitPath) {
  return (
    explicitPath ??
    process.env.REP_FCM_SERVICE_ACCOUNT ??
    config.fcmServiceAccount ??
    process.env.GOOGLE_APPLICATION_CREDENTIALS ??
    process.env.FCM_SERVICE_ACCOUNT
  );
}

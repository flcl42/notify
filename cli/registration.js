import http from 'node:http';
import { addPushRegistration } from './store.js';

function readJsonBody(request) {
  return new Promise((resolve, reject) => {
    let body = '';

    request.on('data', (chunk) => {
      body += chunk;
      if (body.length > 128 * 1024) {
        request.destroy();
        reject(new Error('Request body is too large.'));
      }
    });

    request.on('end', () => {
      try {
        resolve(body.trim() ? JSON.parse(body) : {});
      } catch (error) {
        reject(error);
      }
    });

    request.on('error', reject);
  });
}

function sendJson(response, statusCode, payload) {
  response.writeHead(statusCode, {
    'content-type': 'application/json; charset=utf-8',
    'cache-control': 'no-store'
  });
  response.end(JSON.stringify(payload));
}

export function createRegistrationServer({ subscriptionId, port, host, onRegister }) {
  let resolveRegistration;
  const registered = new Promise((resolve) => {
    resolveRegistration = resolve;
  });

  const server = http.createServer(async (request, response) => {
    const url = new URL(request.url ?? '/', `http://${request.headers.host ?? '127.0.0.1'}`);

    if (request.method === 'GET' && url.pathname === '/health') {
      sendJson(response, 200, { ok: true, subscriptionId });
      return;
    }

    if (request.method !== 'POST' || url.pathname !== '/register') {
      sendJson(response, 404, { ok: false, error: 'Not found.' });
      return;
    }

    try {
      const payload = await readJsonBody(request);
      if (payload.subscriptionId !== subscriptionId) {
        sendJson(response, 403, { ok: false, error: 'Subscription mismatch.' });
        return;
      }

      const token = String(payload.pushToken ?? payload.token ?? '');
      if (!token) {
        sendJson(response, 400, { ok: false, error: 'Missing push token.' });
        return;
      }

      const register = onRegister ?? ((registration) => addPushRegistration(subscriptionId, registration));
      const { subscription } = await register({
        provider: String(payload.provider ?? 'fcm'),
        token,
        platform: String(payload.platform ?? 'unknown'),
        registeredAt: new Date().toISOString()
      });

      sendJson(response, 200, {
        ok: true,
        subscriptionId,
        pushTokenCount: subscription.pushTokens.length
      });
      resolveRegistration(subscription);
    } catch (error) {
      sendJson(response, 400, { ok: false, error: error.message });
    }
  });

  return {
    registered,
    start() {
      return new Promise((resolve, reject) => {
        server.once('error', reject);
        server.listen(port, host, () => {
          server.off('error', reject);
          resolve(server.address());
        });
      });
    },
    close() {
      server.close();
    }
  };
}

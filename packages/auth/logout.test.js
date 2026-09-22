import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createOidcClient } from './index.js';

function setup(t) {
  const values = new Map();
  const redirects = [];
  const tokens = [];
  const replace = (key, value) => {
    const previous = Object.getOwnPropertyDescriptor(globalThis, key);
    Object.defineProperty(globalThis, key, { configurable: true, value });
    t.after(() => {
      if (previous) Object.defineProperty(globalThis, key, previous);
      else delete globalThis[key];
    });
  };
  replace('sessionStorage', {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: (key) => values.delete(key),
  });
  replace('location', { origin: 'http://localhost:5173', assign: (url) => redirects.push(url) });
  values.set(
    'dancehub.oidc.admin-web',
    JSON.stringify({ accessToken: 'old-access', refreshToken: 'refresh' }),
  );
  const client = createOidcClient({
    issuer: 'http://issuer.invalid',
    clientId: 'admin-web',
    onToken: (token) => tokens.push(token),
  });
  return { values, tokens, redirects, client };
}

test('a refresh response arriving after logout cannot restore the session', async (t) => {
  const { client, values, tokens, redirects } = setup(t);
  let finish;
  t.mock.method(
    globalThis,
    'fetch',
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const pending = client.refresh();
  client.logout();
  finish({
    ok: true,
    json: async () => ({
      access_token: 'new-access',
      refresh_token: 'new-refresh',
      expires_in: 3600,
    }),
  });
  assert.equal(await pending, null);
  assert.equal(values.has('dancehub.oidc.admin-web'), false);
  assert.deepEqual(tokens, ['']);
  assert.equal(redirects.length, 1);
});

test('logout also discards a refresh whose body was still being decoded', async (t) => {
  const { client, values, tokens } = setup(t);
  let finishBody;
  let bodyStarted;
  const started = new Promise((resolve) => {
    bodyStarted = resolve;
  });
  t.mock.method(globalThis, 'fetch', async () => ({
    ok: true,
    json: () => {
      bodyStarted();
      return new Promise((resolve) => {
        finishBody = resolve;
      });
    },
  }));
  const pending = client.refresh();
  await started;
  client.logout();
  finishBody({ access_token: 'late-access', expires_in: 3600 });
  assert.equal(await pending, null);
  assert.equal(values.has('dancehub.oidc.admin-web'), false);
  assert.deepEqual(tokens, ['']);
});

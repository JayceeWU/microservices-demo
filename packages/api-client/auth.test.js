import assert from 'node:assert/strict';
import { beforeEach, test } from 'node:test';
import {
  clearAuthContext,
  getAuthContext,
  initializeApiClient,
  request,
  selectStudio,
  setAuthContext,
} from './index.js';

const storage = () => {
  const values = new Map();
  return {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, String(value)),
    removeItem: (key) => values.delete(key),
  };
};

beforeEach(() => {
  Object.defineProperty(globalThis, 'sessionStorage', { configurable: true, value: storage() });
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: storage() });
  globalThis.DANCEHUB_AUTH_MODE = 'oidc';
  clearAuthContext();
});

test('clearing an OIDC token removes old storage and cannot restore Authorization', async (t) => {
  setAuthContext({ accessToken: 'old-token', studioId: 'old-studio' });
  localStorage.setItem('dancehub.dev-auth', JSON.stringify({ userId: 'another-user' }));
  assert.deepEqual(setAuthContext({ accessToken: '', userId: '' }), {
    accessToken: '',
    userId: '',
    studioId: '',
  });
  assert.equal(sessionStorage.getItem('dancehub.auth'), null);
  assert.equal(localStorage.getItem('dancehub.dev-auth'), null);
  assert.equal(getAuthContext().accessToken, '');
  initializeApiClient('https://api.invalid');
  t.mock.method(globalThis, 'fetch', async (_url, options) => {
    assert.equal(options.headers.has('Authorization'), false);
    assert.equal(options.headers.has('X-Studio-Id'), false);
    return { ok: true, json: async () => ({}) };
  });
  await request('/v1/me');
});

test('refresh retains selected studio while logout and another identity do not', () => {
  setAuthContext({ accessToken: 'first-token' });
  selectStudio('studio-a');
  setAuthContext({ accessToken: 'refreshed-token' });
  assert.equal(getAuthContext().studioId, 'studio-a');
  clearAuthContext();
  setAuthContext({ accessToken: 'other-user-token' });
  assert.equal(getAuthContext().studioId, '');
});

test('OIDC and dev storage never merge identities', () => {
  globalThis.DANCEHUB_AUTH_MODE = 'dev';
  setAuthContext({ userId: 'dev-user', studioId: 'dev-studio' });
  assert.equal(getAuthContext().userId, 'dev-user');
  setAuthContext({ accessToken: 'oidc-token', studioId: 'oidc-studio' });
  assert.equal(localStorage.getItem('dancehub.dev-auth'), null);
  assert.deepEqual(getAuthContext(), {
    accessToken: 'oidc-token',
    userId: '',
    studioId: 'oidc-studio',
  });
});

test('malformed storage and dev credentials outside dev mode are ignored', () => {
  sessionStorage.setItem('dancehub.auth', '{broken');
  localStorage.setItem(
    'dancehub.dev-auth',
    JSON.stringify({ userId: 'dev-user', accessToken: 'stale' }),
  );
  assert.deepEqual(getAuthContext(), { userId: '', studioId: '', accessToken: '' });
  assert.equal(sessionStorage.getItem('dancehub.auth'), null);
});

test('OIDC keeps the resolved platform user ID without emitting a dev identity header', async (t) => {
  setAuthContext({ accessToken: 'token' });
  setAuthContext({ userId: 'platform-user-id' });
  selectStudio('studio');
  assert.equal(getAuthContext().userId, 'platform-user-id');
  setAuthContext({ accessToken: 'refreshed' });
  assert.equal(getAuthContext().userId, 'platform-user-id');
  initializeApiClient('https://api.invalid');
  t.mock.method(globalThis, 'fetch', async (_url, options) => {
    assert.equal(options.headers.get('Authorization'), 'Bearer refreshed');
    assert.equal(options.headers.has('X-User-Id'), false);
    return { ok: true, json: async () => ({}) };
  });
  await request('/v1/me');
});

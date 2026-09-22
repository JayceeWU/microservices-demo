import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  api,
  clearAuthContext,
  collectPages,
  initializeApiClient,
  setAuthContext,
} from './index.js';

test('list requests preserve opaque tokens and both boolean filter values', async (t) => {
  initializeApiClient('https://api.invalid');
  const urls = [];
  t.mock.method(globalThis, 'fetch', async (url) => {
    urls.push(new URL(url));
    return { ok: true, json: async () => ({ nextPageToken: 'next' }) };
  });
  assert.equal(
    (await api.sessions({ bookable_only: true, page_size: 24, page_token: 'a+/=' })).nextPageToken,
    'next',
  );
  await api.sessions({ bookable_only: false });
  await api.myBookings({ page_token: 'bookings' });
  await api.roster('session', { page_token: 'roster' });
  assert.equal(urls[0].searchParams.get('page_token'), 'a+/=');
  assert.equal(urls[0].searchParams.get('page_size'), '24');
  assert.equal(urls[0].searchParams.get('bookable_only'), 'true');
  assert.equal(urls[1].searchParams.get('bookable_only'), 'false');
  assert.equal(urls[2].searchParams.get('page_token'), 'bookings');
  assert.equal(urls[3].pathname, '/v1/class-sessions/session/roster');
  assert.equal(urls[3].searchParams.get('page_token'), 'roster');
});

test('calendar page collection follows every cursor and rejects a looping server', async () => {
  const tokens = [];
  const result = await collectPages(async (token) => {
    tokens.push(token);
    return token ? { items: ['last'] } : { items: ['first'], nextPageToken: 'next' };
  });
  assert.deepEqual(result, ['first', 'last']);
  assert.deepEqual(tokens, ['', 'next']);
  await assert.rejects(
    collectPages(async () => ({ items: [], nextPageToken: 'loop' })),
    /repeated page token/,
  );
});

test('session pages and payroll keep their explicit tenant while the global selection changes', async (t) => {
  initializeApiClient('https://api.invalid');
  t.after(clearAuthContext);
  const requests = [];
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    requests.push({ url: new URL(url), studio: options.headers.get('X-Studio-Id') });
    return { ok: true, json: async () => ({}) };
  });
  clearAuthContext();
  await api.monthlyPayroll('queried-studio', '2030-01');
  setAuthContext({ accessToken: 'token', studioId: 'first-selection' });
  await api.sessions({ studio_id: 'queried-studio', teacher_id: 'teacher' });
  setAuthContext({ studioId: 'changed-selection' });
  await api.sessions({ studio_id: 'queried-studio', teacher_id: 'teacher', page_token: 'next' });
  await api.monthlyPayroll('queried-studio', '2030-01');
  assert.deepEqual(
    requests.map((request) => request.studio),
    Array(4).fill('queried-studio'),
  );
  assert.equal(requests[2].url.searchParams.get('page_token'), 'next');
});

test('attendance commands send caller-owned idempotency keys to separate endpoints', async (t) => {
  initializeApiClient('https://api.invalid');
  const sent = [];
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    sent.push({
      path: new URL(url).pathname,
      method: options.method,
      body: JSON.parse(options.body),
    });
    return { ok: true, json: async () => ({}) };
  });
  await api.addWalkIn('session', 'student', 'reason', 'walk-key');
  await api.correctAttendance('booking', false, 'reason', 'attendance-key');
  await api.reverseRedemption('booking', 'reason', 'reverse-key');
  assert.deepEqual(
    sent.map((item) => item.body.idempotencyKey),
    ['walk-key', 'attendance-key', 'reverse-key'],
  );
  assert.equal(sent[1].path, '/v1/bookings/booking/attendance');
  assert.equal(sent[1].method, 'PATCH');
  assert.equal(sent[1].body.attended, false);
  assert.equal(sent[2].path, '/v1/bookings/booking/reverse');
});

import { test, expect } from '@playwright/test';

const apiHeaders = {
  'access-control-allow-origin': '*',
  'access-control-allow-methods': 'GET,OPTIONS',
  'access-control-allow-headers':
    'Authorization,Content-Type,X-Studio-Id,X-Request-Id,Traceparent,Tracestate,Baggage',
};

test('student logout removes browser identity and gates protected pages after reload', async ({
  page,
}) => {
  await page.goto('/profile');
  await expect(page.getByLabel('Display name')).toHaveValue('Mia Student');
  await expect
    .poll(() =>
      page.evaluate(() => JSON.parse(sessionStorage.getItem('dancehub.auth') || '{}').userId),
    )
    .toBe('30000000-0000-0000-0000-000000000001');
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign in with Keycloak' })).toBeVisible();
  await expect.poll(() => page.evaluate(() => sessionStorage.getItem('dancehub.auth'))).toBeNull();
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem('dancehub.dev-auth')))
    .toBeNull();
  const protectedRequests: string[] = [];
  page.on('request', (request) => {
    if (request.url().includes('/v1/me')) protectedRequests.push(request.url());
  });
  await page.reload();
  await expect(page.getByRole('button', { name: 'Sign in with Keycloak' })).toBeVisible();
  expect(protectedRequests).toEqual([]);
  await expect(page.getByLabel('Display name')).toHaveCount(0);
});

test('an unauthenticated session clears stale credentials before protected queries mount', async ({
  page,
}) => {
  await page.route('**/api/auth/session', (route) => route.fulfill({ json: null }));
  await page.addInitScript(() => {
    sessionStorage.setItem(
      'dancehub.auth',
      JSON.stringify({ accessToken: 'stale-token', studioId: 'stale-studio' }),
    );
    localStorage.setItem('dancehub.dev-auth', JSON.stringify({ userId: 'stale-user' }));
  });
  const protectedRequests: string[] = [];
  page.on('request', (request) => {
    if (request.url().includes('/v1/me')) protectedRequests.push(request.url());
  });
  await page.goto('/profile');
  await expect(page.getByRole('button', { name: 'Sign in with Keycloak' })).toBeVisible();
  expect(protectedRequests).toEqual([]);
  expect(await page.evaluate(() => sessionStorage.getItem('dancehub.auth'))).toBeNull();
});

test('identity changes replace cached profile and bind the platform user ID', async ({ page }) => {
  let identity = 'a';
  await page.route('**/api/auth/session', (route) =>
    route.fulfill({
      json: {
        user: { name: identity },
        subject: `oidc-${identity}`,
        accessToken: `token-${identity}`,
        expires: '2099-01-01T00:00:00Z',
      },
    }),
  );
  await page.route('**/v1/me', (route) =>
    route.fulfill({
      headers: apiHeaders,
      json: {
        id: `platform-${identity}`,
        displayName: `Account ${identity}`,
        timezone: 'America/Los_Angeles',
      },
    }),
  );
  await page.route('**/v1/me/memberships', (route) =>
    route.fulfill({ headers: apiHeaders, json: { memberships: [], globalRoles: [] } }),
  );
  await page.goto('/profile');
  await expect(page.getByLabel('Display name')).toHaveValue('Account a');
  identity = 'b';
  await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
  await expect(page.getByLabel('Display name')).toHaveValue('Account b');
  expect(
    await page.evaluate(() => JSON.parse(sessionStorage.getItem('dancehub.auth') || '{}').userId),
  ).toBe('platform-b');
});

test('a failed account bootstrap offers retry and never opens a half-authenticated workspace', async ({
  page,
}) => {
  let fail = true;
  await page.route('**/v1/me', async (route) => {
    if (route.request().method() === 'OPTIONS')
      await route.fulfill({ status: 204, headers: apiHeaders });
    else if (fail)
      await route.fulfill({
        status: 503,
        headers: apiHeaders,
        json: { detail: 'Temporary account failure' },
      });
    else await route.continue();
  });
  await page.goto('/profile');
  await expect(page.getByRole('heading', { name: 'Unable to load your account' })).toBeVisible();
  await expect(page.getByLabel('Display name')).toHaveCount(0);
  fail = false;
  await page.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(page.getByLabel('Display name')).toHaveValue('Mia Student');
});

test('overwriting the original upload URL cannot replace a CLEAN attachment', async ({
  page,
  request,
}) => {
  const original = Buffer.from(
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=',
    'base64',
  );
  await page.goto('/teachers');
  await page.getByRole('button', { name: 'Contact teacher' }).first().click();
  await expect(page).toHaveURL(/\/chat\?conversation=/);
  const prepared = page.waitForResponse(
    (response) =>
      response.url().endsWith('/v1/chat/attachments:prepare') &&
      response.request().method() === 'POST',
  );
  await page
    .getByLabel('Chat image')
    .setInputFiles({ name: 'snapshot.png', mimeType: 'image/png', buffer: original });
  const upload = await (await prepared).json();
  await expect(page.getByText('Attachment ready')).toBeVisible({ timeout: 30_000 });
  const replacement = await request.put(upload.uploadUrl, {
    data: Buffer.from('changed-after-scan'),
    headers: { 'Content-Type': 'image/png' },
  });
  expect(replacement.ok()).toBe(true);
  const gateway = process.env.GATEWAY_URL || 'http://localhost:8080';
  const download = await page.evaluate(
    async ({ gateway, id }) => {
      const auth = JSON.parse(sessionStorage.getItem('dancehub.auth') || '{}');
      const response = await fetch(`${gateway}/v1/chat/attachments/${id}:download`, {
        headers: {
          Authorization: `Bearer ${auth.accessToken}`,
          'X-Studio-Id': auth.studioId || '',
        },
      });
      return { status: response.status, body: await response.json() };
    },
    { gateway, id: upload.attachment.id },
  );
  expect(download.status).toBe(200);
  const bytes = await request.get(download.body.downloadUrl);
  expect(bytes.ok()).toBe(true);
  expect(await bytes.body()).toEqual(original);
});

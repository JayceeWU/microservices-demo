import { test, expect, type Page } from '@playwright/test';
import { authState } from './paths';

async function apiCall(page: Page, path: string, body?: object) {
  return page.evaluate(
    async ({ path, body }) => {
      const auth = JSON.parse(sessionStorage.getItem('dancehub.auth') || '{}');
      const response = await fetch(`http://localhost:8080${path}`, {
        method: body ? 'POST' : 'GET',
        headers: {
          Authorization: `Bearer ${auth.accessToken}`,
          'Content-Type': 'application/json',
          'X-Studio-Id': auth.studioId || '',
        },
        body: body ? JSON.stringify(body) : undefined,
      });
      return { status: response.status, value: await response.json() };
    },
    { path, body },
  );
}

test('checkout retries failed payment, survives reload, denies another owner and refunds every pass', async ({
  page,
  browser,
}, testInfo) => {
  test.setTimeout(120_000);
  await page.goto('/passes');
  await expect(page.getByRole('heading', { name: /passes/i })).toBeVisible();
  const catalog = await apiCall(
    page,
    '/v1/credit-products?studio_id=10000000-0000-0000-0000-000000000001',
  );
  expect(catalog.status).toBe(200);
  const product = catalog.value.products.find(
    (item: { finalSale: boolean; issuerScope: string }) =>
      !item.finalSale && item.issuerScope === 'STUDIO',
  );
  expect(product).toBeTruthy();
  const before = await apiCall(page, '/v1/credits/grants');
  const previousIds = new Set(before.value.grants.map((grant: { id: string }) => grant.id));
  const created = await apiCall(page, '/v1/orders', {
    items: [{ productVersionId: product.id, quantity: 2 }],
    idempotencyKey: crypto.randomUUID(),
  });
  expect(created.status).toBeLessThan(300);
  const orderId = created.value.id;
  await page.goto(`/orders?orderId=${orderId}`);
  await page.getByRole('button', { name: 'Continue payment', exact: true }).click();
  await expect(page.getByText('Local simulation — no real charge.')).toBeVisible();
  await page.getByRole('button', { name: 'Simulate failed payment' }).click();
  await expect(page.getByRole('heading', { name: 'payment failed', exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('button', { name: 'Simulate successful payment' })).toBeEnabled();

  const payment = await apiCall(page, `/v1/orders/${orderId}/payment`);
  expect(payment.status).toBe(200);
  const adminContext = await browser.newContext({ storageState: authState('admin') });
  const admin = await adminContext.newPage();
  try {
    await admin.goto('http://localhost:5173/refunds');
    await expect(admin.getByRole('heading', { name: 'Refund review' })).toBeVisible();
    expect((await apiCall(admin, `/v1/payments/${payment.value.id}`)).status).toBe(404);
    expect((await apiCall(admin, `/v1/orders/${orderId}/payment`)).status).toBe(404);

    await page.getByRole('button', { name: 'Simulate successful payment' }).click();
    await expect(page.getByRole('heading', { name: 'fulfilled', exact: true })).toBeVisible({
      timeout: 40_000,
    });
    const grants = await apiCall(page, '/v1/credits/grants');
    const purchased = grants.value.grants.filter(
      (grant: { id: string; studioId?: string }) =>
        !previousIds.has(grant.id) && grant.studioId === product.studioId,
    );
    expect(purchased).toHaveLength(2);

    for (const width of [1440, 768, 390]) {
      await page.setViewportSize({ width, height: 1000 });
      await expect(page.getByRole('heading', { name: 'Your purchase' })).toBeVisible();
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
      ).toBe(true);
      await page.screenshot({ path: testInfo.outputPath(`checkout-${width}.png`), fullPage: true });
    }
    await page.getByRole('button', { name: 'Request full refund' }).click();
    await expect(page.getByText('awaiting approval', { exact: true })).toBeVisible({
      timeout: 25_000,
    });
    await admin.getByRole('textbox', { name: /Order ID/ }).fill(orderId);
    await admin.getByLabel('Decision reason').fill('Regression: full unused quantity refund');
    await admin.getByRole('button', { name: 'Approve full original-method refund' }).click();
    await expect(page.getByRole('heading', { name: 'refunded', exact: true })).toBeVisible({
      timeout: 40_000,
    });
    await expect(page.getByText('Payment: refunded', { exact: true })).toBeVisible();
    const after = await apiCall(page, '/v1/credits/grants');
    for (const grant of purchased) {
      expect(after.value.grants.find((item: { id: string }) => item.id === grant.id)?.status).toBe(
        'REVOKED',
      );
    }
  } finally {
    await adminContext.close();
  }
});

test('student checkout recovers a lost create response without a duplicate order', async ({
  page,
}) => {
  await page.goto('/passes');
  await page
    .getByRole('button', { name: /add to cart/i })
    .first()
    .click();
  await expect(page.getByText('Added to cart', { exact: true })).toBeVisible();
  await page.goto('/cart');
  let createdOrder = '';
  await page.route('**/v1/orders', async (route) => {
    const response = await route.fetch();
    createdOrder = (await response.json()).id;
    await route.abort('connectionreset');
  });
  await page.getByRole('button', { name: 'Continue to checkout' }).click();
  await expect(page.locator('.error')).toBeVisible();
  expect(createdOrder).toBeTruthy();
  await page.unroute('**/v1/orders');
  await page.reload();
  await page.getByRole('button', { name: 'Continue to checkout' }).click();
  await expect(page).toHaveURL(new RegExp(`/orders\\?orderId=${createdOrder}$`));
  await expect(page.getByRole('heading', { name: 'Your purchase' })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('button', { name: 'Continue payment', exact: true })).toBeVisible();
});

test('room checkout completes a real reservation with simulated payment', async ({ page }) => {
  test.setTimeout(75_000);
  await page.goto('/rooms');
  await expect(page.getByRole('button', { name: 'Create hold & payment' })).toBeEnabled();
  const start = new Date(Date.now() + 40 * 24 * 60 * 60 * 1000);
  start.setMinutes(0, 0, 0);
  const end = new Date(start.getTime() + 60 * 60 * 1000);
  await page.getByLabel('Starts', { exact: true }).fill(start.toISOString().slice(0, 16));
  await page.getByLabel('Ends', { exact: true }).fill(end.toISOString().slice(0, 16));
  await page.getByRole('button', { name: 'Create hold & payment' }).click();
  await expect(page).toHaveURL(/\/orders\?orderId=/);
  await page.getByRole('button', { name: 'Continue payment', exact: true }).click();
  await page.getByRole('button', { name: 'Simulate successful payment' }).click();
  await expect(page.getByRole('heading', { name: 'fulfilled', exact: true })).toBeVisible({
    timeout: 40_000,
  });
});

test('flash sale queue recovers its request URL and reaches a payable order', async ({ page }) => {
  test.setTimeout(75_000);
  await page.goto('/flash-sale');
  await page.getByLabel('Campaign ID').fill('50000000-0000-0000-0000-000000000001');
  await page.getByRole('button', { name: 'Join queue' }).click();
  await expect(page).toHaveURL(/\/flash-sale\?requestId=/);
  await page.reload();
  await page.getByRole('link', { name: 'Continue to payment' }).click();
  await page.getByRole('button', { name: 'Continue payment', exact: true }).click();
  await page.getByRole('button', { name: 'Simulate successful payment' }).click();
  await expect(page.getByRole('heading', { name: 'fulfilled', exact: true })).toBeVisible({
    timeout: 40_000,
  });
});

test('sign out removes the previous browser identity', async ({ page }) => {
  await page.goto('/orders');
  await expect(page.getByRole('button', { name: 'Sign out', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('button', { name: /sign in with keycloak/i })).toBeVisible();
  expect(
    await page.evaluate(() => ({
      auth: sessionStorage.getItem('dancehub.auth'),
      dev: localStorage.getItem('dancehub.dev-auth'),
    })),
  ).toEqual({ auth: null, dev: null });
});

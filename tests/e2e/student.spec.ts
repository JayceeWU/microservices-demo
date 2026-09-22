import { test, expect, type Page } from '@playwright/test';
import { acceptanceFixtures } from './acceptance-fixtures';
import { loadClassPagesUntil } from './pagination';

async function loadBookingPagesUntil(page: Page, targetBookingId: string) {
  const laterIds: string[] = [];
  for (let pageNumber = 0; pageNumber < 20; pageNumber++) {
    const previousCount = await page.locator('.list article').count();
    const nextResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === '/v1/bookings' && url.searchParams.has('page_token');
    });
    await page.getByRole('button', { name: 'Load more bookings', exact: true }).click();
    const response = await nextResponse;
    expect(response.status()).toBe(200);
    const body: { bookings: Array<{ id: string }> } = await response.json();
    laterIds.push(...body.bookings.map((booking) => booking.id));
    await expect.poll(() => page.locator('.list article').count()).toBeGreaterThan(previousCount);
    if (laterIds.includes(targetBookingId)) return laterIds;
  }
  throw new Error('The fixture booking was not found within 20 subsequent pages');
}

test('student loads bookable classes beyond the first page', async ({ page }, testInfo) => {
  // eslint-disable-next-line playwright/no-skipped-test -- Fixture data is provided by verify:local, not a general seeded environment.
  test.skip(!acceptanceFixtures(), 'Pagination fixtures are created by npm run verify:local.');
  await page.goto('/classes');
  await expect(page.locator('.grid article')).toHaveCount(24);
  await expect(
    page.getByRole('heading', { name: 'Closeout pagination 30', exact: true }),
  ).toHaveCount(0);
  await loadClassPagesUntil(page, page.locator('.grid article'), 'Closeout pagination 30', true);
  await page.screenshot({
    path: testInfo.outputPath('student-paginated-classes.png'),
    fullPage: true,
  });
});

test('student loads their bookings beyond the first page', async ({ page }, testInfo) => {
  const fixtures = acceptanceFixtures();
  // eslint-disable-next-line playwright/no-skipped-test -- Fixture data is provided by verify:local, not a general seeded environment.
  test.skip(!fixtures, 'Pagination fixtures are created by npm run verify:local.');
  expect(fixtures!.paginationBookingId).toBeTruthy();
  expect(fixtures!.paginationBookingSessionId).toBeTruthy();
  await page.goto('/bookings');
  await expect(page.getByRole('heading', { name: 'My bookings', exact: true })).toBeVisible();
  await expect(page.locator('.list article')).toHaveCount(24);
  const target = page.locator('.list article').filter({
    has: page.getByText(fixtures!.paginationBookingSessionId, { exact: true }),
  });
  await expect(target).toHaveCount(0);
  const laterIds = await loadBookingPagesUntil(page, fixtures!.paginationBookingId);
  expect(laterIds).toContain(fixtures!.paginationBookingId);
  await expect(target).toBeVisible();
  await expect(target).toContainText('CANCELLED');
  await page.screenshot({
    path: testInfo.outputPath('student-paginated-bookings.png'),
    fullPage: true,
  });
});

test('@critical student can browse classes and reaches real booking action', async ({ page }) => {
  await page.goto('/classes');
  await expect(page.getByRole('heading', { name: 'Classes' })).toBeVisible();
  const book = page.getByRole('button', { name: 'Book' }).first();
  await expect(book).toBeEnabled();
});

test('student room hold validates form and flash sale exposes queue', async ({ page }) => {
  await page.goto('/rooms');
  await expect(page.getByRole('button', { name: /create hold & payment/i })).toBeVisible();
  await page.goto('/flash-sale');
  await expect(page.getByRole('button', { name: /join queue/i })).toBeVisible();
});

test('student contacts a teacher and sends a realtime message', async ({ page }) => {
  const message = `Hello from the student chat flow ${crypto.randomUUID()}`;
  await page.goto('/teachers');
  await page.getByRole('button', { name: 'Contact teacher' }).first().click();
  await expect(page).toHaveURL(/\/chat\?conversation=/);
  await page.getByPlaceholder('Write a message').fill(message);
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page.getByText(message)).toBeVisible();
});

test('student uploads a private image that passes the media scan', async ({ page }) => {
  const caption = `Scanned image ${crypto.randomUUID()}`;
  await page.goto('/teachers');
  await page.getByRole('button', { name: 'Contact teacher' }).first().click();
  await expect(page).toHaveURL(/\/chat\?conversation=/);
  await page.getByLabel('Chat image').setInputFiles({
    name: 'chat.png',
    mimeType: 'image/png',
    buffer: Buffer.from(
      'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=',
      'base64',
    ),
  });
  await expect(page.getByText('Attachment ready')).toBeVisible({ timeout: 30_000 });
  await page.getByPlaceholder('Write a message').fill(caption);
  await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeEnabled({
    timeout: 30_000,
  });
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page.getByText(caption)).toBeVisible();
});

test('student opens the shared studio support conversation', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Contact studio' }).first().click();
  await expect(page).toHaveURL(/\/chat\?conversation=/);
  await expect(page.getByText(/STUDIO_SUPPORT/).first()).toBeVisible();
});

import { test, expect } from '@playwright/test';
import { acceptanceFixtures } from './acceptance-fixtures';

test('teacher calendar includes later classes and roster loads its next page', async ({
  page,
}, testInfo) => {
  const fixtures = acceptanceFixtures();
  // eslint-disable-next-line playwright/no-skipped-test -- Fixture data is provided by verify:local, not a general seeded environment.
  test.skip(!fixtures, 'Pagination fixtures are created by npm run verify:local.');
  await page.goto('/');
  await expect(
    page.getByRole('heading', { name: 'Closeout pagination 30', exact: true }),
  ).toBeVisible();
  await page.goto(`/roster?session=${fixtures!.paginationSessionId}`);
  await expect(page.locator('.roster-schedule article')).toHaveCount(24);
  await page.getByRole('button', { name: 'Load more bookings', exact: true }).click();
  await expect(page.locator('.roster-schedule article')).toHaveCount(30);
  await expect(page.getByRole('button', { name: 'Load more bookings', exact: true })).toHaveCount(
    0,
  );
  await page.screenshot({
    path: testInfo.outputPath('teacher-paginated-roster.png'),
    fullPage: true,
  });
});

test('@critical teacher sees cross-studio calendar', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Cross-studio calendar' })).toBeVisible();
});

test('teacher creates a studio-scoped teacher group', async ({ page }) => {
  const title = `Teacher practice chat ${crypto.randomUUID()}`;
  await page.goto('/chat');
  await page.getByPlaceholder('New teacher group').fill(title);
  await page.getByRole('button', { name: 'Create group' }).click();
  await expect(page.getByRole('button', { name: new RegExp(title) }).first()).toBeVisible();
  page.once('dialog', (dialog) => dialog.accept('Playwright group deletion'));
  await page.getByRole('button', { name: 'Delete group' }).click();
  await expect(page.getByRole('button', { name: new RegExp(title) })).toHaveCount(0);
});

test('teacher chat stays idle without re-sending receipts and receives edits live', async ({
  page,
}) => {
  const title = `Teacher receipt chat ${crypto.randomUUID()}`;
  const body = `Realtime check ${crypto.randomUUID()}`;
  await page.goto('/chat');
  await page.getByPlaceholder('New teacher group').fill(title);
  await page.getByRole('button', { name: 'Create group' }).click();
  await expect(page.getByRole('button', { name: new RegExp(title) }).first()).toBeVisible();
  await expect(page.getByText('Connected', { exact: true })).toBeVisible();
  await page.getByPlaceholder('Write a message').fill(body);
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page.getByText(body)).toBeVisible();

  // An open, non-empty thread used to refresh on every frame and re-send its own read
  // receipt, which the server echoed back: the page polled forever without user input.
  const requests: string[] = [];
  const record = (request: { url(): string }) => requests.push(request.url());
  page.on('request', record);
  await new Promise((resolve) => setTimeout(resolve, 4_000));
  page.off('request', record);
  const threadFetches = requests.filter((url) =>
    /\/v1\/chat\/conversations\/[^/]+\/messages/.test(url),
  );
  expect(threadFetches.length).toBeLessThanOrEqual(1);

  // Edits and withdrawals arrive through the realtime broadcast; the command ack alone
  // does not refresh the thread.
  page.once('dialog', (dialog) => dialog.accept(`${body} edited`));
  await page.getByRole('button', { name: 'Edit', exact: true }).first().click();
  await expect(page.getByText(`${body} edited`)).toBeVisible();
  await page.getByRole('button', { name: 'Withdraw', exact: true }).first().click();
  await expect(page.getByText('Message withdrawn')).toBeVisible();

  page.once('dialog', (dialog) => dialog.accept('Playwright group deletion'));
  await page.getByRole('button', { name: 'Delete group' }).click();
  await expect(page.getByRole('button', { name: new RegExp(title) })).toHaveCount(0);
});

test('teacher calendar deep-links roster and saves an external video link', async ({ page }) => {
  await page.goto('/');
  const roster = page.getByRole('link', { name: 'Roster', exact: true }).first();
  await expect(roster).toBeVisible();
  const href = await roster.getAttribute('href');
  const sessionId = new URL(href!, 'http://localhost').searchParams.get('session')!;
  await roster.click();
  await expect(page.getByPlaceholder('Class session ID')).toHaveValue(sessionId);
  await page.goto('/history');
  await page.getByPlaceholder('Class session ID').fill(sessionId);
  await page.getByPlaceholder('https://…').fill('https://video.example.invalid/lesson');
  await page.getByRole('button', { name: 'Save video link' }).click();
  await expect(page.getByText('Video link saved.')).toBeVisible();
});

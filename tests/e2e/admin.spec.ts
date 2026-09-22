import { test, expect } from '@playwright/test';
import { authState } from './paths';
import { acceptanceFixtures } from './acceptance-fixtures';
import { loadClassPagesUntil } from './pagination';

test('admin loads later classes and reads the completed class payroll projection', async ({
  page,
}, testInfo) => {
  const fixtures = acceptanceFixtures();
  // eslint-disable-next-line playwright/no-skipped-test -- Fixture data is provided by verify:local, not a general seeded environment.
  test.skip(!fixtures, 'Payroll and pagination fixtures are created by npm run verify:local.');
  await page.goto('/');
  await expect(page.locator('article.row')).toHaveCount(24);
  await expect(
    page.getByRole('heading', { name: 'Closeout pagination 30', exact: true }),
  ).toHaveCount(0);
  await loadClassPagesUntil(page, page.locator('article.row'), 'Closeout pagination 30');
  await page.goto('/payroll');
  await page.getByLabel('Payroll month').fill(fixtures!.payrollMonth);
  const line = page.locator(`article[data-session-id="${fixtures!.payrollSessionId}"]`);
  await expect(line).toContainText(`$${(fixtures!.payrollAmountCents / 100).toFixed(2)}`);
  const refresh = await page.waitForResponse(
    (response) => response.url().includes('/v1/payroll') && response.request().method() === 'GET',
    { timeout: 12_000 },
  );
  expect(refresh.status()).toBe(200);
  await expect(line).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath('admin-payroll-projection.png'),
    fullPage: true,
  });
});

test('admin retries a pending redemption reversal with the same key and keeps no-show separate', async ({
  page,
}) => {
  const keys: string[] = [];
  await page.route('**/v1/bookings/*/reverse', async (route) => {
    keys.push(route.request().postDataJSON().idempotencyKey);
    await route.fulfill({
      status: keys.length === 1 ? 503 : 200,
      contentType: 'application/json',
      body: JSON.stringify(
        keys.length === 1 ? { detail: 'Credits are processing' } : { id: 'booking' },
      ),
    });
  });
  await page.goto('/attendance');
  await expect(
    page.getByLabel('Attendance status').getByRole('option', { name: 'No show', exact: true }),
  ).toHaveCount(1);
  const form = page
    .locator('form')
    .filter({ has: page.getByRole('heading', { name: 'Reverse redemption', exact: true }) });
  await form.getByLabel('bookingId').fill('70000000-0000-0000-0000-000000000001');
  await form.getByLabel('Audit reason').fill('Regression pending response');
  await form.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect(
    page.getByText('Request received; processing credits. Retrying automatically.'),
  ).toBeVisible();
  await expect(page.getByText('Operation completed', { exact: true })).toBeVisible();
  expect(keys).toHaveLength(2);
  expect(keys[0]).toBeTruthy();
  expect(keys[1]).toBe(keys[0]);
});

test('admin no-show preserves payroll and real reversal updates the open payroll page', async ({
  page,
}, testInfo) => {
  const fixtures = acceptanceFixtures();
  // eslint-disable-next-line playwright/no-skipped-test -- Fixture data is provided by verify:local, not a general seeded environment.
  test.skip(!fixtures, 'Attendance and payroll fixtures are created by npm run verify:local.');
  expect(fixtures!.attendanceSessionId).toBeTruthy();
  expect(fixtures!.attendanceBookingId).toBeTruthy();
  await page.goto('/payroll');
  await page.getByLabel('Payroll month').fill(fixtures!.attendanceMonth);
  const payrollLine = page.locator(`article[data-session-id="${fixtures!.attendanceSessionId}"]`);
  await expect(payrollLine).toContainText('$34.00');

  const attendance = await page.context().newPage();
  try {
    await attendance.goto('/attendance');
    const correction = attendance.locator('form').filter({
      has: attendance.getByRole('heading', { name: 'Correct attendance', exact: true }),
    });
    await correction.getByLabel('Booking ID').fill(fixtures!.attendanceBookingId);
    await correction.getByLabel('Attendance status').selectOption('false');
    await correction.getByLabel('Audit reason').fill('Browser regression: record a real no-show');
    const correctedResponse = attendance.waitForResponse(
      (response) =>
        new URL(response.url()).pathname ===
          `/v1/bookings/${fixtures!.attendanceBookingId}/attendance` &&
        response.request().method() === 'PATCH' &&
        response.status() === 200,
    );
    await correction.getByRole('button', { name: 'Apply correction', exact: true }).click();
    expect((await (await correctedResponse).json()).status).toBe('NO_SHOW');
    await expect(attendance.getByText('Operation completed', { exact: true })).toBeVisible();

    const refreshedPayroll = page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname === '/v1/payroll/monthly' && response.status() === 200,
    );
    await page.bringToFront();
    const noShowPayroll = await (await refreshedPayroll).json();
    const noShowLine = noShowPayroll.lines.find(
      (line: { classSessionId: string }) => line.classSessionId === fixtures!.attendanceSessionId,
    );
    expect(noShowLine).toBeTruthy();
    expect(Number(noShowLine.totalAmountCents)).toBe(3400);
    await expect(payrollLine).toContainText('$34.00');

    await attendance.bringToFront();
    const reversal = attendance.locator('form').filter({
      has: attendance.getByRole('heading', { name: 'Reverse redemption', exact: true }),
    });
    await reversal.getByLabel('bookingId').fill(fixtures!.attendanceBookingId);
    await reversal
      .getByLabel('Audit reason')
      .fill('Browser regression: reverse the real redemption');
    const reversedResponse = attendance.waitForResponse(
      (response) =>
        new URL(response.url()).pathname ===
          `/v1/bookings/${fixtures!.attendanceBookingId}/reverse` &&
        response.request().method() === 'POST' &&
        response.status() === 200,
    );
    await reversal.getByRole('button', { name: 'Submit', exact: true }).click();
    expect((await (await reversedResponse).json()).status).toBe('REVERSED');
    await expect(attendance.getByText('Operation completed', { exact: true })).toBeVisible();
    // Return to the existing page; its query resumes polling without navigation or reload.
    await page.bringToFront();
    await expect(payrollLine).toContainText('$30.00', { timeout: 20_000 });
    await page.screenshot({
      path: testInfo.outputPath('admin-real-reversal-payroll.png'),
      fullPage: true,
    });
  } finally {
    await attendance.close();
  }
});

test('@critical admin switches tenant and opens operations overview', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByLabel('Current studio')).toBeVisible();
  await expect(page.getByText('Operational overview')).toBeVisible();
});

test('studio admin creates a studio group from the shared inbox', async ({ page }) => {
  const title = `Studio announcements chat ${crypto.randomUUID()}`;
  await page.goto('/chat');
  await page.getByLabel('New studio group').fill(title);
  await page.getByRole('button', { name: 'Create group' }).click();
  await expect(page.getByRole('button', { name: new RegExp(title) }).first()).toBeVisible();
  await page.getByLabel('Chat image or video').setInputFiles({
    name: 'group-cleanup.png',
    mimeType: 'image/png',
    buffer: Buffer.from(
      'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=',
      'base64',
    ),
  });
  await expect(page.getByText('Attachment ready')).toBeVisible({ timeout: 30_000 });
  page.once('dialog', (dialog) => dialog.accept('Playwright group deletion'));
  await page.getByRole('button', { name: 'Delete group' }).click();
  await expect(page.getByRole('button', { name: new RegExp(title) })).toHaveCount(0);
});

test('studio admin inbox lists a support conversation the admin has not joined', async ({
  browser,
  page,
}) => {
  // A student contacts HA Dance Studio (the demo admin's studio). The admin is not a
  // participant of that STUDIO_SUPPORT conversation yet, and the inbox must still load:
  // the missing participant row used to scan as NULL and fail the whole list.
  const student = await browser.newContext({
    baseURL: process.env.STUDENT_WEB_URL || 'http://localhost:3000',
    storageState: authState('student'),
  });
  const studentPage = await student.newPage();
  await studentPage.goto('/');
  await studentPage
    .locator('article.studio', { hasText: 'HA Dance Studio' })
    .getByRole('button', { name: 'Contact studio' })
    .click();
  await expect(studentPage).toHaveURL(/\/chat\?conversation=/);
  await student.close();

  const inbox = page.waitForResponse(
    (response) =>
      response.url().endsWith('/v1/chat/conversations') && response.request().method() === 'GET',
  );
  await page.goto('/chat');
  expect((await inbox).status()).toBe(200);
  const support = page.getByRole('button', { name: /STUDIO_SUPPORT/ }).first();
  await expect(support).toBeVisible();
  await support.click();
  await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeVisible();
});

test('studio admin cannot silently obtain platform page', async ({ page }) => {
  await page.goto('/platform');
  await expect(page.getByRole('heading', { name: 'Forbidden' })).toBeVisible();
  await expect(page.getByText('Platform administrator role is required.')).toBeVisible();
});

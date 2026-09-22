import { expect, test as setup, Page } from '@playwright/test';
import { mkdir } from 'node:fs/promises';
import { authDir, authState } from './paths';

const password = process.env.E2E_KEYCLOAK_PASSWORD || 'DanceHub123!';
const users = {
  student: process.env.E2E_STUDENT_USER || 'student@bayareadancehub.local',
  teacher: process.env.E2E_TEACHER_USER || 'teacher@bayareadancehub.local',
  admin: process.env.E2E_ADMIN_USER || 'admin@bayareadancehub.local',
  platform: process.env.E2E_PLATFORM_USER || 'platform@bayareadancehub.local',
};
const origins = {
  student: process.env.STUDENT_WEB_URL || 'http://localhost:3000',
  teacher: process.env.TEACHER_WEB_URL || 'http://localhost:4200',
  admin: process.env.ADMIN_WEB_URL || 'http://localhost:5173',
};

async function submitKeycloakLogin(page: Page, username: string) {
  await expect(page).toHaveURL(/localhost:8081\/realms\/bay-area-dance-hub/);
  await page.getByLabel(/username|email/i).fill(username);
  await page.locator('#password').fill(password);
  await page.getByRole('button', { name: /sign in/i }).click();
}

async function login(page: Page, kind: keyof typeof users) {
  page.on('pageerror', (error) => console.error(`[${kind}] browser error: ${error.message}`));
  page.on('requestfailed', (request) =>
    console.error(
      `[${kind}] request failed: ${request.url()} ${request.failure()?.errorText || ''}`,
    ),
  );
  const origin =
    kind === 'student' ? origins.student : kind === 'teacher' ? origins.teacher : origins.admin;
  await page.goto(origin);
  if (kind === 'student')
    await page.getByRole('button', { name: /sign in with keycloak/i }).click();
  await submitKeycloakLogin(page, users[kind]);
  const escaped = origin.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  await expect(page).toHaveURL(new RegExp(`^${escaped}`), { timeout: 15_000 });
  await expect(page.getByRole('button', { name: /sign out/i })).toBeVisible({ timeout: 15_000 });
}

setup('create real Keycloak role storage states', async ({ browser }) => {
  await mkdir(authDir, { recursive: true });
  const allKinds = ['student', 'teacher', 'admin', 'platform'] as const;
  const selected = allKinds.filter(
    (kind) => !process.env.E2E_AUTH_KIND || kind === process.env.E2E_AUTH_KIND,
  );
  expect(selected.length).toBeGreaterThan(0);
  for (const kind of selected) {
    const context = await browser.newContext();
    const page = await context.newPage();
    await login(page, kind);
    await context.storageState({ path: authState(kind) });
    await context.close();
  }
});

import { test, expect } from '@playwright/test';

test('platform admin opens the authenticated cross-tenant workspace', async ({ page }) => {
  await page.goto('/platform');
  await expect(page.getByRole('heading', { name: 'Platform administration' })).toBeVisible();
  await expect(page.getByText('Cross-tenant view.')).toBeVisible();
  await expect(page.getByRole('link', { name: 'Platform admin' })).toBeVisible();
  await page.getByLabel('Exact email address').fill('platform@bayareadancehub.local');
  await page.getByRole('button', { name: 'Look up user' }).click();
  await expect(page.getByText('Already a platform admin')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Grant platform administrator' })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Cannot revoke yourself' })).toBeDisabled();
});

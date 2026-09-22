import { expect, type Locator, type Page } from '@playwright/test';

export async function loadClassPagesUntil(
  page: Page,
  rows: Locator,
  title: string,
  bookableOnly = false,
) {
  const seenTokens = new Set<string>();
  for (let pageNumber = 0; pageNumber < 20; pageNumber++) {
    const previousCount = await rows.count();
    const nextResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return (
        response.request().method() === 'GET' &&
        url.pathname === '/v1/class-sessions' &&
        Boolean(url.searchParams.get('page_token'))
      );
    });
    await page.getByRole('button', { name: 'Load more classes', exact: true }).click();
    const response = await nextResponse;
    expect(response.status()).toBe(200);
    const params = new URL(response.url()).searchParams;
    const token = params.get('page_token')!;
    expect(token).toBeTruthy();
    expect(seenTokens.has(token)).toBe(false);
    seenTokens.add(token);
    expect(params.get('bookable_only')).toBe(bookableOnly ? 'true' : null);
    const body: { sessions: Array<{ title: string }> } = await response.json();
    expect(body.sessions.length).toBeGreaterThan(0);
    await expect(rows).toHaveCount(previousCount + body.sessions.length);
    if (body.sessions.some((session) => session.title === title)) {
      await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible();
      return;
    }
  }
  throw new Error(`The fixture class ${title} was not found within 20 subsequent pages`);
}

import { join, resolve } from 'node:path';

export const authDir = resolve(process.env.PLAYWRIGHT_AUTH_DIR || 'tests/e2e/.auth');
export const authState = (role: 'student' | 'teacher' | 'admin' | 'platform') =>
  join(authDir, `${role}.json`);
export const outputDir = resolve(process.env.PLAYWRIGHT_OUTPUT_DIR || 'test-results/playwright');

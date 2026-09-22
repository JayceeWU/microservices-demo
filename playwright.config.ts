import { defineConfig, devices } from '@playwright/test';
import { authState, outputDir } from './tests/e2e/paths';

export default defineConfig({
  testDir: './tests/e2e',
  timeout: 45_000,
  fullyParallel: true,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 2 : undefined,
  reporter: process.env.CI
    ? [
        ['html', { open: 'never', outputFolder: `${outputDir}-report` }],
        ['junit', { outputFile: `${outputDir}.xml` }],
      ]
    : 'list',
  use: { trace: 'retain-on-failure', screenshot: 'only-on-failure', video: 'retain-on-failure' },
  projects: [
    { name: 'auth-setup', testMatch: /auth\.setup\.ts/ },
    {
      name: 'student-desktop',
      testMatch: /(?:student|transactions)\.spec\.ts/,
      dependencies: ['auth-setup'],
      use: {
        ...devices['Desktop Chrome'],
        baseURL: process.env.STUDENT_WEB_URL || 'http://localhost:3000',
        storageState: authState('student'),
      },
    },
    {
      name: 'student-mobile',
      testMatch: /student\.spec\.ts/,
      grep: /@critical/,
      dependencies: ['auth-setup'],
      use: {
        ...devices['Pixel 7'],
        baseURL: process.env.STUDENT_WEB_URL || 'http://localhost:3000',
        storageState: authState('student'),
      },
    },
    {
      name: 'teacher-desktop',
      testMatch: /teacher\.spec\.ts/,
      dependencies: ['auth-setup'],
      use: {
        ...devices['Desktop Chrome'],
        baseURL: process.env.TEACHER_WEB_URL || 'http://localhost:4200',
        storageState: authState('teacher'),
      },
    },
    {
      name: 'admin-desktop',
      testMatch: /admin\.spec\.ts/,
      dependencies: ['auth-setup'],
      use: {
        ...devices['Desktop Chrome'],
        baseURL: process.env.ADMIN_WEB_URL || 'http://localhost:5173',
        storageState: authState('admin'),
      },
    },
    {
      name: 'admin-mobile',
      testMatch: /admin\.spec\.ts/,
      grep: /@critical/,
      dependencies: ['auth-setup'],
      use: {
        ...devices['Pixel 7'],
        baseURL: process.env.ADMIN_WEB_URL || 'http://localhost:5173',
        storageState: authState('admin'),
      },
    },
    {
      name: 'platform-admin',
      testMatch: /platform\.spec\.ts/,
      dependencies: ['auth-setup'],
      use: {
        ...devices['Desktop Chrome'],
        baseURL: process.env.ADMIN_WEB_URL || 'http://localhost:5173',
        storageState: authState('platform'),
      },
    },
  ],
  outputDir,
});

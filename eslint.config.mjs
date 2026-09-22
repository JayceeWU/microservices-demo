import js from '@eslint/js';
import angular from 'angular-eslint';
import { defineConfig, globalIgnores } from 'eslint/config';
import nextCoreWebVitals from 'eslint-config-next/core-web-vitals';
import eslintConfigPrettier from 'eslint-config-prettier';
import nextTypescript from 'eslint-config-next/typescript';
import jsxA11y from 'eslint-plugin-jsx-a11y';
import playwright from 'eslint-plugin-playwright';
import react from 'eslint-plugin-react';
import reactHooks from 'eslint-plugin-react-hooks';
import globals from 'globals';
import tseslint from 'typescript-eslint';

const javascriptFiles = [
  'apps/**/*.{js,jsx,mjs,cjs}',
  'packages/**/*.{js,jsx,mjs,cjs}',
  'src/paymentservice/**/*.{js,mjs,cjs}',
];
const typescriptFiles = [
  'apps/**/*.{ts,tsx}',
  'packages/**/*.{ts,tsx}',
  'tests/e2e/**/*.ts',
  'playwright.config.ts',
];
const studentFiles = ['apps/student-web/**/*.{js,jsx,ts,tsx,mjs,cjs}'];
const adminFiles = ['apps/admin-web/**/*.{js,jsx,ts,tsx,mjs,cjs}'];
const teacherFiles = ['apps/teacher-web/**/*.ts'];
const playwrightFiles = ['tests/e2e/**/*.ts', 'playwright.config.ts'];

const nextConfigs = [...nextCoreWebVitals, ...nextTypescript].map((config) => ({
  ...config,
  files: studentFiles,
  rules: {
    ...config.rules,
    '@next/next/no-html-link-for-pages': 'off',
  },
}));

export default defineConfig([
  globalIgnores([
    '**/node_modules/**',
    '**/.next/**',
    '**/dist/**',
    '**/bin/**',
    '**/obj/**',
    '**/.angular/**',
    '**/.cache/**',
    '**/coverage/**',
    '**/playwright-report/**',
    '**/test-results/**',
    '**/load-results/**',
    '**/gen/**',
    '**/Generated/**',
    '**/*.generated.*',
    '**/*.pb.*',
    '**/*_pb2*.py',
    '**/*.d.ts',
    'packages/api-client/openapi.d.ts',
  ]),
  {
    name: 'dancehub/javascript',
    files: javascriptFiles,
    ...js.configs.recommended,
    languageOptions: {
      ...js.configs.recommended.languageOptions,
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: { ...globals.browser, ...globals.node, ...globals.es2025 },
    },
  },
  ...tseslint.configs.recommended.map((config) => ({
    ...config,
    files: typescriptFiles,
  })),
  ...nextConfigs,
  {
    name: 'dancehub/react',
    files: adminFiles,
    ...react.configs.flat.recommended,
    languageOptions: {
      ...react.configs.flat.recommended.languageOptions,
      globals: { ...globals.browser, ...globals.es2025 },
    },
    settings: { react: { version: 'detect' } },
    rules: {
      ...react.configs.flat.recommended.rules,
      'react/prop-types': 'off',
    },
  },
  {
    name: 'dancehub/react-jsx-runtime',
    files: adminFiles,
    ...react.configs.flat['jsx-runtime'],
  },
  {
    name: 'dancehub/react-hooks',
    files: adminFiles,
    ...reactHooks.configs.flat.recommended,
  },
  {
    name: 'dancehub/jsx-accessibility',
    files: adminFiles,
    ...jsxA11y.flatConfigs.recommended,
  },
  {
    name: 'dancehub/angular',
    files: teacherFiles,
    extends: [...angular.configs.tsRecommended],
    processor: angular.processInlineTemplates,
  },
  {
    name: 'dancehub/playwright',
    files: playwrightFiles,
    ...playwright.configs['flat/recommended'],
  },
  {
    name: 'dancehub/payment-commonjs',
    files: ['src/paymentservice/**/*.js'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'commonjs',
      globals: { ...globals.node, ...globals.es2025 },
    },
  },
  eslintConfigPrettier,
]);

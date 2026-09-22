import assert from 'node:assert/strict';
import test from 'node:test';
import { resolveAdminWorkspace } from './workspace-policy.js';

test('studio administrators only see their active administrator memberships', () => {
  const result = resolveAdminWorkspace(
    {
      memberships: [
        { studioId: 'a', role: 'studio_admin', active: true },
        { studioId: 'b', role: 'student', active: true },
        { studioId: 'c', role: 'studio_admin', active: false },
        { studioId: '', role: 'studio_admin', active: true },
      ],
      globalRoles: [],
    },
    [],
    'b',
  );
  assert.deepEqual(
    result.allowed.map((item) => item.studioId),
    ['a'],
  );
  assert.equal(result.platformAdmin, false);
  assert.equal(result.defaultStudioId, 'a');
});

test('platform administrators use the platform studio list even without memberships', () => {
  const result = resolveAdminWorkspace(
    { memberships: [], globalRoles: ['platform_admin'] },
    [
      { id: 'a', name: 'A' },
      { id: 'b', name: 'B' },
    ],
    'b',
  );
  assert.equal(result.platformAdmin, true);
  assert.deepEqual(result.studioOptions, [
    { studioId: 'a', studioName: 'A' },
    { studioId: 'b', studioName: 'B' },
  ]);
  assert.equal(result.defaultStudioId, 'b');
});

test('an empty authorization response selects no studio', () => {
  assert.equal(resolveAdminWorkspace(undefined, undefined, '').defaultStudioId, '');
  assert.deepEqual(
    resolveAdminWorkspace({ memberships: [], globalRoles: ['platform_admin'] }, undefined, '')
      .studioOptions,
    [],
  );
});

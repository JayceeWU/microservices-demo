import assert from 'node:assert/strict';
import test from 'node:test';
import { activeTeacherMemberships, initialStudio } from './workspace-policy.js';

test('teacher workspace excludes other roles and inactive memberships', () => {
  const allowed = activeTeacherMemberships([
    { studioId: 'a', role: 'teacher', active: true },
    { studioId: 'b', role: 'student', active: true },
    { studioId: 'c', role: 'teacher', active: false },
  ]);
  assert.deepEqual(
    allowed.map((item) => item.studioId),
    ['a'],
  );
  assert.equal(initialStudio(allowed, 'missing'), 'a');
});

test('current studio is retained only when it remains authorized', () => {
  const allowed = activeTeacherMemberships([
    { studioId: 'a', role: 'teacher', active: true },
    { studioId: 'b', role: 'teacher', active: true },
  ]);
  assert.equal(initialStudio(allowed, 'b'), 'b');
  assert.deepEqual(activeTeacherMemberships(undefined), []);
  assert.equal(initialStudio([], 'b'), '');
});

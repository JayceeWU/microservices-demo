import assert from 'node:assert/strict';
import test from 'node:test';
import { activeStudioOptions, defaultStudio } from './workspace-policy.js';

test('studio options are active, selectable and deduplicated across tenant roles', () => {
  const options = activeStudioOptions([
    { studioId: 'a', studioName: 'Alpha', role: 'student', active: true },
    { studioId: 'a', studioName: 'Alpha', role: 'teacher', active: true },
    { studioId: 'b', role: 'student', active: false },
    { studioId: 'b', role: 'student', active: true },
    { studioId: '', role: 'student', active: true },
  ]);
  assert.deepEqual(options, [
    { studioId: 'a', studioName: 'Alpha' },
    { studioId: 'b', studioName: 'b' },
  ]);
  assert.equal(defaultStudio(options, 'a'), 'a');
  assert.equal(defaultStudio(options, 'missing'), 'a');
});

test('empty membership state selects no studio', () => {
  assert.deepEqual(activeStudioOptions(undefined), []);
  assert.equal(defaultStudio([], 'a'), '');
});

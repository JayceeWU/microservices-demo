import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createAttendanceCommands } from './attendance-commands.js';

test('lost and pending responses replay the same key; success and changed contents start a new operation', async () => {
  const keys = [];
  let sequence = 0;
  let failure = true;
  const run = createAttendanceCommands(
    {
      addWalkIn: async (...args) => {
        keys.push(args.at(-1));
        if (failure) throw Object.assign(new Error('processing'), { status: 503 });
        return { id: 'booking' };
      },
    },
    () => `key-${++sequence}`,
  );
  const value = { sessionId: 'session', studentId: 'student', reason: 'correction' };
  await assert.rejects(run('walk', value));
  await assert.rejects(run('walk', { ...value }));
  await assert.rejects(run('walk', { ...value, reason: 'changed reason' }));
  failure = false;
  await run('walk', { ...value, reason: 'changed reason' });
  await run('walk', { ...value, reason: 'changed reason' });
  assert.deepEqual(keys, ['key-1', 'key-1', 'key-2', 'key-2', 'key-3']);
});

test('no-show correction and credit reversal are distinct operations with independent retries', async () => {
  const sent = [];
  const api = {
    correctAttendance: async (...args) => {
      sent.push(['correct', ...args]);
      throw new Error('lost');
    },
    reverseRedemption: async (...args) => {
      sent.push(['reverse', ...args]);
      throw new Error('lost');
    },
  };
  let sequence = 0;
  const run = createAttendanceCommands(api, () => `key-${++sequence}`);
  const value = { bookingId: 'booking', attended: 'false', reason: 'reason' };
  await assert.rejects(run('correct', value));
  await assert.rejects(run('reverse', { bookingId: 'booking', reason: 'reason' }));
  await assert.rejects(run('correct', value));
  assert.equal(sent[0][2], false);
  assert.deepEqual(
    sent.map((args) => args.at(-1)),
    ['key-1', 'key-2', 'key-1'],
  );
});

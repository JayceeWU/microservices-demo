import assert from 'node:assert/strict';
import { test } from 'node:test';
import { chatFrameScope, ChatSocket } from './index.js';

const openSocket = (socket) => {
  const sent = [];
  socket.socket = { readyState: WebSocket.OPEN, send: (frame) => sent.push(JSON.parse(frame)) };
  return sent;
};

test('markRead only sends receipts that advance the conversation', () => {
  const socket = new ChatSocket();
  const sent = openSocket(socket);
  assert.equal(socket.markRead('conv-1', 5), true);
  assert.equal(socket.markRead('conv-1', 5), false);
  assert.equal(socket.markRead('conv-1', 3), false);
  assert.equal(socket.markRead('conv-1', 6), true);
  assert.equal(socket.markRead('conv-2', 1), true);
  assert.deepEqual(
    sent.map((frame) => frame.markRead),
    [
      { conversationId: 'conv-1', sequence: 5 },
      { conversationId: 'conv-1', sequence: 6 },
      { conversationId: 'conv-2', sequence: 1 },
    ],
  );
});

test('markRead is a no-op while disconnected and remembers nothing', () => {
  const socket = new ChatSocket();
  assert.equal(socket.markRead('conv-1', 5), false);
  const sent = openSocket(socket);
  assert.equal(socket.markRead('conv-1', 5), true);
  assert.equal(sent.length, 1);
});

test('frame scope refreshes the thread only for content changes', () => {
  assert.equal(chatFrameScope({ typing: { conversationId: 'c', typing: true } }), 'none');
  assert.equal(chatFrameScope({ presence: { userId: 'u', online: true } }), 'none');
  assert.equal(chatFrameScope({ ack: { commandId: 'x' } }), 'none');
  assert.equal(
    chatFrameScope({ readUpdated: { conversationId: 'c', sequence: 2 } }),
    'conversations',
  );
  assert.equal(chatFrameScope({ messageCreated: { id: 'm' } }), 'thread');
  assert.equal(chatFrameScope({ messageUpdated: { id: 'm' } }), 'thread');
  assert.equal(chatFrameScope({ messageWithdrawnId: 'm' }), 'thread');
  assert.equal(chatFrameScope({ attachmentUpdated: { id: 'a' } }), 'thread');
  assert.equal(chatFrameScope({ error: { code: 'X' } }), 'thread');
  assert.equal(chatFrameScope({ ready: true }), 'thread');
  assert.equal(chatFrameScope(null), 'none');
});

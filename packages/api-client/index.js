let API_BASE = '';

let authContext = { userId: '', studioId: '', accessToken: '' };

export class ApiError extends Error {
  constructor(status, problem) {
    super(
      problem.detail ||
        problem.message ||
        problem.title ||
        problem.error ||
        `Request failed: ${status}`,
    );
    this.name = 'ApiError';
    this.status = status;
    this.code = problem.type || problem.error || 'request_failed';
    this.problem = problem;
  }
}

export function getAuthContext() {
  // Stored identities are alternatives, never layers to merge across users.
  if (typeof sessionStorage !== 'undefined') {
    try {
      const stored = JSON.parse(sessionStorage.getItem('dancehub.auth') || 'null');
      if (stored?.accessToken) {
        authContext = {
          userId: stored.userId || '',
          studioId: stored.studioId || '',
          accessToken: stored.accessToken,
        };
        return { ...authContext };
      }
    } catch {
      sessionStorage.removeItem('dancehub.auth');
    }
  }
  if (typeof localStorage !== 'undefined' && globalThis.DANCEHUB_AUTH_MODE === 'dev') {
    try {
      const stored = JSON.parse(localStorage.getItem('dancehub.dev-auth') || 'null');
      if (stored?.userId) {
        authContext = { userId: stored.userId, studioId: stored.studioId || '', accessToken: '' };
      }
    } catch {
      localStorage.removeItem('dancehub.dev-auth');
    }
  }
  return { ...authContext };
}

export function setAuthContext(next) {
  const clearingToken = Object.hasOwn(next, 'accessToken') && !next.accessToken;
  const current = clearingToken ? { userId: '', studioId: '', accessToken: '' } : getAuthContext();
  clearAuthContext();
  authContext = { ...current, ...next };
  if (authContext.accessToken && typeof sessionStorage !== 'undefined') {
    authContext.userId = next.userId ?? (current.accessToken ? current.userId : '');
    sessionStorage.setItem('dancehub.auth', JSON.stringify(authContext));
  } else if (
    authContext.userId &&
    globalThis.DANCEHUB_AUTH_MODE === 'dev' &&
    typeof localStorage !== 'undefined'
  ) {
    localStorage.setItem('dancehub.dev-auth', JSON.stringify(authContext));
  }
  return { ...authContext };
}

export function clearAuthContext() {
  authContext = { userId: '', studioId: '', accessToken: '' };
  if (typeof sessionStorage !== 'undefined') sessionStorage.removeItem('dancehub.auth');
  if (typeof localStorage !== 'undefined') localStorage.removeItem('dancehub.dev-auth');
}

export function selectStudio(studioId) {
  return setAuthContext({ studioId });
}

export function initializeApiClient(apiBase) {
  if (!apiBase) throw new Error('DANCEHUB_API_URL is required');
  API_BASE = apiBase.replace(/\/$/, '');
}

function queryString(values = {}) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) {
    if (value !== '' && value !== undefined && value !== null) query.set(key, String(value));
  }
  const encoded = query.toString();
  return encoded ? `?${encoded}` : '';
}

export async function request(path, options = {}) {
  if (!API_BASE) throw new Error('API client is not initialized');
  const headers = new Headers(options.headers || {});
  headers.set('Accept', 'application/json');
  const body =
    options.body && typeof options.body !== 'string' ? JSON.stringify(options.body) : options.body;
  if (body) headers.set('Content-Type', 'application/json');
  const auth = getAuthContext();
  if (auth.accessToken) headers.set('Authorization', `Bearer ${auth.accessToken}`);
  if (auth.studioId && !headers.has('X-Studio-Id')) headers.set('X-Studio-Id', auth.studioId);
  headers.set('X-Request-Id', headers.get('X-Request-Id') || crypto.randomUUID());
  const devHeadersAllowed =
    typeof location !== 'undefined' &&
    ['localhost', '127.0.0.1'].includes(location.hostname) &&
    globalThis.DANCEHUB_AUTH_MODE === 'dev';
  if (!auth.accessToken && devHeadersAllowed && auth.userId) {
    headers.set('X-User-Id', auth.userId);
  }
  const response = await fetch(`${API_BASE}${path}`, { ...options, body, headers });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new ApiError(response.status, payload);
  return payload;
}

const command = (path, body = {}, method = 'POST') =>
  request(path, {
    method,
    body: { ...body, idempotencyKey: body.idempotencyKey || crypto.randomUUID() },
  });

// Calendar/history views need the complete collection before filtering or sorting.
export async function collectPages(load) {
  const items = [];
  const seen = new Set();
  let token = '';
  do {
    const page = await load(token);
    items.push(...page.items);
    token = page.nextPageToken || '';
    if (token && seen.has(token)) throw new Error('The server returned a repeated page token');
    seen.add(token);
  } while (token);
  return items;
}

export const api = {
  profile: () => request('/v1/me'),
  updateProfile: (value) => command('/v1/me', value, 'PATCH'),
  memberships: () => request('/v1/me/memberships'),
  lookupPlatformUser: (email) => request('/v1/platform/users:lookup' + queryString({ email })),
  globalRoleAssignments: () => request('/v1/platform/global-role-assignments'),
  grantGlobalRole: (userId, reason) =>
    command('/v1/platform/global-role-assignments', { userId, role: 'platform_admin', reason }),
  revokeGlobalRole: (assignmentId, reason) =>
    command(`/v1/platform/global-role-assignments/${assignmentId}:revoke`, { reason }),
  studios: (query) => request('/v1/studios' + queryString({ query })),
  studio: (id) => request(`/v1/studios/${id}`),
  rooms: (studioId) => request(`/v1/studios/${studioId}/rooms`),
  teachers: (values) => request('/v1/teachers' + queryString(values)),
  products: (studioId) => request('/v1/credit-products' + queryString({ studio_id: studioId })),
  sessions: (values) =>
    request('/v1/class-sessions' + queryString(values), {
      headers: values?.studio_id ? { 'X-Studio-Id': values.studio_id } : undefined,
    }),
  session: (id) => request(`/v1/class-sessions/${id}`),
  myBookings: (values) => request('/v1/bookings' + queryString(values)),
  bookClass: (sessionId, idempotencyKey) =>
    command(`/v1/class-sessions/${sessionId}/bookings`, { idempotencyKey }),
  cancelBooking: (bookingId, reason) => command(`/v1/bookings/${bookingId}/cancel`, { reason }),
  submitSchedule: (value) => command('/v1/class-sessions', value),
  approveSchedule: (sessionId, value) => command(`/v1/class-sessions/${sessionId}/approve`, value),
  roster: (sessionId, values) =>
    request(`/v1/class-sessions/${sessionId}/roster` + queryString(values)),
  correctAttendance: (bookingId, attended, reason, idempotencyKey) =>
    command(`/v1/bookings/${bookingId}/attendance`, { attended, reason, idempotencyKey }, 'PATCH'),
  addWalkIn: (sessionId, studentId, reason, idempotencyKey) =>
    command(`/v1/class-sessions/${sessionId}/walk-ins`, { studentId, reason, idempotencyKey }),
  reverseRedemption: (bookingId, reason, idempotencyKey) =>
    command(`/v1/bookings/${bookingId}/reverse`, { reason, idempotencyKey }),
  completeClass: (sessionId, reason) =>
    command(`/v1/class-sessions/${sessionId}/complete`, { reason }),
  cancelClass: (sessionId, reason) => command(`/v1/class-sessions/${sessionId}/cancel`, { reason }),
  updateClassVideo: (sessionId, videoUrl, reason) =>
    command(`/v1/class-sessions/${sessionId}/video`, { videoUrl, reason }, 'PATCH'),
  balances: () => request('/v1/credits/balances'),
  grants: () => request('/v1/credits/grants'),
  ledger: (values) => request('/v1/credits/ledger' + queryString(values)),
  transferGrant: (grantId, targetUserId, reason) =>
    command(`/v1/credits/grants/${grantId}/transfer`, { targetUserId, reason }),
  pauseGrant: (grantId, reason) => command(`/v1/credits/grants/${grantId}/pause`, { reason }),
  resumeGrant: (grantId, reason) => command(`/v1/credits/grants/${grantId}/resume`, { reason }),
  cart: () => request('/v1/cart'),
  addCartItem: (productVersionId, quantity = 1) =>
    command('/v1/cart/items', { productVersionId, quantity }),
  removeCartItem: (productVersionId) =>
    request(`/v1/cart/items/${productVersionId}`, { method: 'DELETE' }),
  createOrder: (items, idempotencyKey) => command('/v1/orders', { items, idempotencyKey }),
  order: (id) => request(`/v1/orders/${id}`),
  createPayment: (orderId, idempotencyKey) => command('/v1/payments', { orderId, idempotencyKey }),
  payment: (id) => request(`/v1/payments/${id}`),
  paymentForOrder: (orderId) => request(`/v1/orders/${orderId}/payment`),
  simulatePayment: (id, outcome, idempotencyKey) =>
    command(`/v1/payments/${id}/simulate`, { outcome, idempotencyKey }),
  requestRefund: (id, reason) => command(`/v1/orders/${id}/refund`, { reason }),
  approveRefund: (id, reason) => command(`/v1/orders/${id}/refund/approve`, { reason }),
  createRoomHold: (value) => command('/v1/room-reservations', value),
  createRoomOrder: (roomReservationId, idempotencyKey) =>
    command('/v1/orders/room', { roomReservationId, idempotencyKey }),
  cancelRoomReservation: (id, reason) => command(`/v1/room-reservations/${id}/cancel`, { reason }),
  reserveFlashSale: (campaignId, idempotencyKey) =>
    command(`/v1/flash-sales/${campaignId}/reservations`, { idempotencyKey }),
  flashSaleRequest: (requestId) => request(`/v1/flash-sale-requests/${requestId}`),
  inviteTeacher: (studioId, email) => command('/v1/teachers/invitations', { studioId, email }),
  monthlyPayroll: (studioId, month, teacherId = '') =>
    request(
      '/v1/payroll/monthly' + queryString({ studio_id: studioId, month, teacher_id: teacherId }),
      { headers: { 'X-Studio-Id': studioId } },
    ),
  analytics: (studioId) => request('/v1/analytics' + queryString({ studio_id: studioId })),
  chatConversations: () => request('/v1/chat/conversations'),
  chatGroups: (values = {}) => request('/v1/chat/groups' + queryString(values)),
  createDirectConversation: (recipientUserId) =>
    command('/v1/chat/direct-conversations', { recipientUserId }),
  createStudioConversation: (studioId, studentId = '') =>
    command('/v1/chat/studio-conversations', { studioId, studentId }),
  createChatGroup: (value) => command('/v1/chat/groups', value),
  deleteChatGroup: (conversationId, reason) =>
    command(`/v1/chat/groups/${conversationId}:delete`, { reason }),
  joinChatGroup: (conversationId) => command(`/v1/chat/groups/${conversationId}:join`),
  leaveChatGroup: (conversationId) => command(`/v1/chat/groups/${conversationId}:leave`),
  banChatMember: (conversationId, userId, reason) =>
    command(`/v1/chat/groups/${conversationId}/members/${userId}:ban`, { reason }),
  unbanChatMember: (conversationId, userId, reason) =>
    command(`/v1/chat/groups/${conversationId}/members/${userId}:unban`, { reason }),
  chatMessages: (conversationId, beforeSequence = '') =>
    request(
      `/v1/chat/conversations/${conversationId}/messages` +
        queryString({ before_sequence: beforeSequence }),
    ),
  blockChatUser: (blockedUserId) => command('/v1/chat/blocks', { blockedUserId }),
  unblockChatUser: (blockedUserId) =>
    request(`/v1/chat/blocks/${blockedUserId}`, { method: 'DELETE' }),
  prepareChatAttachment: (value) => command('/v1/chat/attachments:prepare', value),
  completeChatAttachment: (attachmentId) =>
    command('/v1/chat/attachments:complete', { attachmentId }),
  chatAttachmentDownload: (attachmentId) =>
    request(`/v1/chat/attachments/${attachmentId}:download`),
  uploadChatAttachment: async (conversationId, file, onProgress = () => undefined) => {
    const hash = [
      ...new Uint8Array(await crypto.subtle.digest('SHA-256', await file.arrayBuffer())),
    ]
      .map((value) => value.toString(16).padStart(2, '0'))
      .join('');
    const kind = file.type === 'video/mp4' ? 'MESSAGE_KIND_VIDEO' : 'MESSAGE_KIND_IMAGE';
    const prepared = await api.prepareChatAttachment({
      conversationId,
      kind,
      mimeType: file.type,
      sizeBytes: file.size,
      sha256: hash,
    });
    await new Promise((resolve, reject) => {
      const upload = new XMLHttpRequest();
      upload.open('PUT', prepared.uploadUrl);
      upload.upload.addEventListener('progress', (event) => {
        if (event.lengthComputable) onProgress(Math.round((event.loaded / event.total) * 100));
      });
      upload.addEventListener('load', () =>
        upload.status >= 200 && upload.status < 300
          ? resolve(undefined)
          : reject(new Error(`Upload failed: ${upload.status}`)),
      );
      upload.addEventListener('error', () => reject(new Error('Upload failed')));
      upload.send(file);
    });
    const attachment = await api.completeChatAttachment(prepared.attachment.id);
    for (let attempt = 0; attempt < 60; attempt += 1) {
      await new Promise((resolve) => globalThis.setTimeout(resolve, 1000));
      try {
        await api.chatAttachmentDownload(attachment.id);
        return { ...attachment, status: 'ATTACHMENT_STATUS_CLEAN' };
      } catch (error) {
        if (attempt === 59) throw error;
      }
    }
  },
  reportChatMessage: (messageId, reason) =>
    command(`/v1/chat/messages/${messageId}:report`, { reason }),
  createChatTicket: () => command('/v1/chat/ws-ticket'),
};

// Which cached views a server frame invalidates for a page that shows a conversation list
// and one open thread. Typing, presence and command acks change neither; a read receipt only
// moves unread counters. Refreshing on every frame made pages re-send their own receipt,
// which the server echoed back, so an idle thread kept polling.
export function chatFrameScope(frame) {
  if (!frame || typeof frame !== 'object') return 'none';
  if ('typing' in frame || 'presence' in frame || 'ack' in frame) return 'none';
  if ('readUpdated' in frame) return 'conversations';
  // `ready` opens every (re)connection: resync the thread for anything missed while offline.
  return 'thread';
}

export class ChatSocket extends EventTarget {
  constructor() {
    super();
    this.socket = null;
    this.closed = false;
    this.retry = 500;
    this.heartbeat = 0;
    // Highest sequence already reported per conversation; receipts never go backwards.
    this.readReceipts = new Map();
  }
  get connected() {
    return this.socket?.readyState === WebSocket.OPEN;
  }
  async connect() {
    this.closed = false;
    // A page may call connect() again while a previous socket is still open (for example
    // after switching conversations); replace it instead of keeping two sockets alive.
    const previous = this.socket;
    this.socket = null;
    if (previous && previous.readyState <= WebSocket.OPEN) previous.close(1000, 'replaced');
    const { ticket } = await api.createChatTicket();
    if (this.closed) return;
    const base = API_BASE.replace(/^http/, 'ws');
    const socket = new WebSocket(`${base}/v1/chat/ws?ticket=${encodeURIComponent(ticket)}`);
    this.socket = socket;
    socket.addEventListener('open', () => {
      if (this.socket !== socket) return;
      this.retry = 500;
      globalThis.clearInterval(this.heartbeat);
      this.heartbeat = globalThis.setInterval(
        () => this.send({ version: 1, commandId: crypto.randomUUID(), heartbeat: true }),
        25_000,
      );
      this.dispatchEvent(new Event('connected'));
    });
    socket.addEventListener('message', (event) => {
      if (this.socket !== socket) return;
      const frame = JSON.parse(String(event.data));
      this.dispatchEvent(new CustomEvent('frame', { detail: frame }));
    });
    socket.addEventListener('close', () => {
      // Only the current socket drives reconnection; a replaced socket closing late must
      // not spawn a second connection.
      if (this.socket !== socket) return;
      globalThis.clearInterval(this.heartbeat);
      this.dispatchEvent(new Event('disconnected'));
      if (!this.closed) {
        const delay = this.retry;
        this.retry = Math.min(this.retry * 2, 15_000);
        globalThis.setTimeout(() => {
          if (!this.closed && this.socket === socket) this.connect().catch(() => undefined);
        }, delay);
      }
    });
  }
  send(frame) {
    if (!this.connected) throw new Error('Chat is reconnecting');
    this.socket.send(JSON.stringify(frame));
  }
  // Presence-style commands are best effort: dropping one while reconnecting is harmless,
  // whereas sendMessage() must surface the failure so the draft is not lost.
  sendIfConnected(frame) {
    if (this.connected) this.socket.send(JSON.stringify(frame));
  }
  sendMessage(conversationId, body, attachmentIds = [], kind = '', replyToMessageId = '') {
    const commandId = crypto.randomUUID();
    this.send({
      version: 1,
      commandId,
      sendMessage: {
        conversationId,
        clientMessageId: crypto.randomUUID(),
        kind: kind || (attachmentIds.length ? 'MESSAGE_KIND_IMAGE' : 'MESSAGE_KIND_TEXT'),
        body,
        attachmentIds,
        replyToMessageId,
      },
    });
    return commandId;
  }
  editMessage(messageId, body) {
    this.send({ version: 1, commandId: crypto.randomUUID(), editMessage: { messageId, body } });
  }
  withdrawMessage(messageId) {
    this.send({ version: 1, commandId: crypto.randomUUID(), withdrawMessage: { messageId } });
  }
  // Sends a read receipt only when it advances past the last one sent on this socket, so a
  // page may call it after every refresh without generating traffic. Returns whether it sent.
  markRead(conversationId, sequence) {
    if (!conversationId || !this.connected) return false;
    if (!(sequence > (this.readReceipts.get(conversationId) ?? 0))) return false;
    this.readReceipts.set(conversationId, sequence);
    this.send({
      version: 1,
      commandId: crypto.randomUUID(),
      markRead: { conversationId, sequence },
    });
    return true;
  }
  typing(conversationId, typing) {
    this.sendIfConnected({
      version: 1,
      commandId: crypto.randomUUID(),
      typing: { conversationId, typing },
    });
  }
  close() {
    this.closed = true;
    globalThis.clearInterval(this.heartbeat);
    this.socket?.close(1000, 'signed out');
  }
}

export { API_BASE };

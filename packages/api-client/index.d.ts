import type { components } from './openapi';

declare global {
  var DANCEHUB_API_URL: string;
  var DANCEHUB_OIDC_ISSUER: string;
  var DANCEHUB_AUTH_MODE: string;
  var DANCEHUB_ENVIRONMENT: string;
}

export type JsonValue =
  string | number | boolean | null | JsonValue[] | { [key: string]: JsonValue };
export interface PageQuery {
  page_size?: number;
  page_token?: string;
}
export function collectPages<T>(
  load: (pageToken: string) => Promise<{ items: T[]; nextPageToken?: string }>,
): Promise<T[]>;
export interface AuthContext {
  userId: string;
  studioId: string;
  accessToken: string;
}
export interface ProblemDetails {
  type?: string;
  title?: string;
  status?: number;
  detail?: string;
  error?: string;
  message?: string;
  [key: string]: JsonValue | undefined;
}
export class ApiError extends Error {
  status: number;
  code: string;
  problem: ProblemDetails;
}

type Schema = components['schemas'];
export type Profile = Schema['v1Profile'];
export type Membership = Schema['v1Membership'] & { studioName?: string };
export type GlobalRole = 'platform_admin';
export interface PlatformUser {
  id: string;
  email: string;
  displayName: string;
}
export interface GlobalRoleAssignment {
  id: string;
  user: PlatformUser;
  role: GlobalRole;
  active: boolean;
  grantedAt?: string;
  revokedAt?: string;
}
export type Studio = Schema['v1Studio'];
export type Teacher = Schema['v1Teacher'];
export type Room = Omit<Schema['v1Room'], 'rentalRateCentsPerHour'> & {
  rentalRateCentsPerHour: number;
};
export type CreditProduct = Omit<
  Schema['v1CreditProductVersion'],
  'issuerScope' | 'kind' | 'amountCents' | 'creditAmount'
> & {
  issuerScope: 'STUDIO' | 'PLATFORM';
  kind: 'CREDITS' | 'UNLIMITED';
  amountCents: number;
  creditAmount: number;
  currency: 'USD';
};
export type ClassStatus =
  | 'PENDING_APPROVAL'
  | 'APPROVED'
  | 'OPEN'
  | 'MINIMUM_CONFIRMED'
  | 'AWAITING_ADMIN_CONFIRMATION'
  | 'COMPLETED'
  | 'CANCELLED';
export type BookingStatus =
  'PENDING_CREDIT' | 'CONFIRMED' | 'CHARGED' | 'ATTENDED' | 'NO_SHOW' | 'CANCELLED' | 'REVERSED';
export type ClassSession = Omit<Schema['v1ClassSession'], 'status' | 'timeRange' | 'creditCost'> & {
  status: ClassStatus;
  startsAt: string;
  endsAt: string;
  creditCost: number;
};
export type Booking = Omit<Schema['v1Booking'], 'status'> & {
  status: BookingStatus;
  title?: string;
  studentName?: string;
  creditCompensationPending?: boolean;
};
export type GrantStatus = 'ACTIVE' | 'PAUSED' | 'TRANSFERRED' | 'REVOKED';
export type Grant = Omit<Schema['v1Grant'], 'status' | 'remaining'> & {
  status: GrantStatus;
  remainingCredits: number;
};
export type LedgerEntry = Omit<Schema['v1LedgerEntry'], 'kind'> & { kind: string };
export type OrderStatus =
  | 'DRAFT'
  | 'PENDING_PAYMENT'
  | 'PAID'
  | 'PAID_NOT_FULFILLED'
  | 'FULFILLED'
  | 'PAYMENT_FAILED'
  | 'EXPIRED'
  | 'REFUND_PENDING'
  | 'REFUNDED';
export type OrderLine = {
  id: string;
  description: string;
  unitAmountCents: number;
  currency: 'USD';
} & (
  | { type: 'CREDIT_PRODUCT'; productVersionId: string; quantity: number; finalSale: boolean }
  | { type: 'ROOM_RESERVATION'; roomReservationId: string }
);
export type Order = Omit<Schema['v1Order'], 'id' | 'status' | 'total' | 'lines'> & {
  id: string;
  status: OrderStatus;
  totalAmountCents: number;
  currency: 'USD';
  lines: OrderLine[];
  refundPhase?: string;
  refundFailureReason?: string;
};
export type RoomReservation = Omit<
  Schema['v1RoomReservation'],
  'id' | 'status' | 'timeRange' | 'amount'
> & {
  id: string;
  status: string;
  startsAt: string;
  endsAt: string;
  amountCents: number;
  currency: 'USD';
};
export type CartItem = Omit<Schema['v1CartItem'], 'productVersionId' | 'quantity'> & {
  productVersionId: string;
  quantity: number;
};
export type Cart = Omit<Schema['v1Cart'], 'items'> & { items: CartItem[] };
export type Payment = Omit<Schema['v1Payment'], 'id' | 'orderId' | 'amountCents'> & {
  id: string;
  orderId: string;
  amountCents: number;
  simulationEnabled?: boolean;
  succeededAt?: string;
  paymentExpiresAt?: string;
};
export type ChatConversation = Schema['v1Conversation'];
export type ChatMessage = Schema['v1Message'];
export type ChatParticipant = Schema['v1Participant'];
export type ChatAttachment = Schema['v1Attachment'];
export type ChatServerFrame = Record<string, JsonValue>;

export function getAuthContext(): AuthContext;
export function setAuthContext(value: Partial<AuthContext>): AuthContext;
export function clearAuthContext(): void;
export function selectStudio(studioId: string): AuthContext;
export function initializeApiClient(apiBase: string): void;
export function request<T = JsonValue>(
  path: string,
  options?: RequestInit & { body?: BodyInit | JsonValue },
): Promise<T>;

export interface ApiClient {
  profile(): Promise<Profile>;
  updateProfile(value: Partial<Profile>): Promise<Profile>;
  memberships(): Promise<{ memberships: Membership[]; globalRoles: GlobalRole[] }>;
  lookupPlatformUser(email: string): Promise<{ user: PlatformUser; globalRoles: GlobalRole[] }>;
  globalRoleAssignments(): Promise<{ assignments: GlobalRoleAssignment[]; nextPageToken?: string }>;
  grantGlobalRole(userId: string, reason: string): Promise<GlobalRoleAssignment>;
  revokeGlobalRole(assignmentId: string, reason: string): Promise<GlobalRoleAssignment>;
  studios(query?: string): Promise<{ studios: Studio[] }>;
  studio(id: string): Promise<Studio>;
  rooms(studioId: string): Promise<{ rooms: Room[] }>;
  teachers(values?: Record<string, string>): Promise<{ teachers: Teacher[] }>;
  products(studioId: string): Promise<{ products: CreditProduct[] }>;
  sessions(
    values?: PageQuery & { studio_id?: string; teacher_id?: string; bookable_only?: boolean },
  ): Promise<{ sessions: ClassSession[]; nextPageToken?: string }>;
  session(id: string): Promise<ClassSession>;
  myBookings(values?: PageQuery): Promise<{ bookings: Booking[]; nextPageToken?: string }>;
  bookClass(sessionId: string, idempotencyKey?: string): Promise<Booking>;
  cancelBooking(bookingId: string, reason: string): Promise<Booking>;
  submitSchedule(value: Record<string, JsonValue>): Promise<ClassSession>;
  approveSchedule(sessionId: string, value: Record<string, JsonValue>): Promise<ClassSession>;
  roster(
    sessionId: string,
    values?: PageQuery,
  ): Promise<{ bookings: Booking[]; roster?: Booking[]; nextPageToken?: string }>;
  correctAttendance(
    bookingId: string,
    attended: boolean,
    reason: string,
    idempotencyKey?: string,
  ): Promise<Booking>;
  addWalkIn(
    sessionId: string,
    studentId: string,
    reason: string,
    idempotencyKey?: string,
  ): Promise<Booking>;
  reverseRedemption(bookingId: string, reason: string, idempotencyKey?: string): Promise<Booking>;
  completeClass(sessionId: string, reason: string): Promise<ClassSession>;
  cancelClass(sessionId: string, reason: string): Promise<ClassSession>;
  updateClassVideo(sessionId: string, videoUrl: string, reason: string): Promise<ClassSession>;
  balances(): Promise<{
    balances: Array<{ studioId: string; availableCredits: number; unlimitedActive: boolean }>;
  }>;
  grants(): Promise<{ grants: Grant[] }>;
  ledger(values?: Record<string, string>): Promise<{ entries: LedgerEntry[] }>;
  transferGrant(
    grantId: string,
    targetUserId: string,
    reason: string,
  ): Promise<Schema['v1GrantOperationResponse']>;
  pauseGrant(grantId: string, reason: string): Promise<Schema['v1GrantOperationResponse']>;
  resumeGrant(grantId: string, reason: string): Promise<Schema['v1GrantOperationResponse']>;
  cart(): Promise<Cart>;
  addCartItem(productVersionId: string, quantity?: number): Promise<Cart>;
  removeCartItem(productVersionId: string): Promise<Cart>;
  createOrder(
    items: Array<{ productVersionId: string; quantity: number }>,
    idempotencyKey?: string,
  ): Promise<Order>;
  order(id: string): Promise<Order>;
  createPayment(orderId: string, idempotencyKey?: string): Promise<Payment>;
  payment(id: string): Promise<Payment>;
  paymentForOrder(orderId: string): Promise<Payment>;
  simulatePayment(
    id: string,
    outcome: 'SUCCEEDED' | 'FAILED',
    idempotencyKey?: string,
  ): Promise<Payment>;
  requestRefund(id: string, reason: string): Promise<Order>;
  approveRefund(id: string, reason: string): Promise<Order>;
  createRoomHold(value: Record<string, JsonValue>): Promise<RoomReservation>;
  createRoomOrder(roomReservationId: string, idempotencyKey?: string): Promise<Order>;
  cancelRoomReservation(id: string, reason: string): Promise<Order>;
  reserveFlashSale(
    campaignId: string,
    idempotencyKey?: string,
  ): Promise<{ requestId: string; campaignId: string; status: string }>;
  flashSaleRequest(requestId: string): Promise<{
    requestId: string;
    campaignId: string;
    status: string;
    orderId?: string;
    reason?: string;
  }>;
  inviteTeacher(studioId: string, email: string): Promise<JsonValue>;
  monthlyPayroll(
    studioId: string,
    month: string,
    teacherId?: string,
  ): Promise<Schema['v1GetMonthlyPayrollResponse']>;
  analytics(studioId: string): Promise<Schema['v1GetStudioAnalyticsResponse']>;
  chatConversations(): Promise<{
    conversations: ChatConversation[];
    page?: Schema['v1PageResponse'];
  }>;
  chatGroups(
    values?: Record<string, string>,
  ): Promise<{ groups: ChatConversation[]; page?: Schema['v1PageResponse'] }>;
  createDirectConversation(recipientUserId: string): Promise<ChatConversation>;
  createStudioConversation(studioId: string, studentId?: string): Promise<ChatConversation>;
  createChatGroup(value: Record<string, JsonValue>): Promise<ChatConversation>;
  deleteChatGroup(conversationId: string, reason: string): Promise<Record<string, never>>;
  joinChatGroup(conversationId: string): Promise<ChatConversation>;
  leaveChatGroup(conversationId: string): Promise<ChatConversation>;
  banChatMember(conversationId: string, userId: string, reason: string): Promise<ChatParticipant>;
  unbanChatMember(conversationId: string, userId: string, reason: string): Promise<ChatParticipant>;
  chatMessages(
    conversationId: string,
    beforeSequence?: string,
  ): Promise<{ messages: ChatMessage[]; page?: Schema['v1PageResponse'] }>;
  blockChatUser(blockedUserId: string): Promise<Schema['v1BlockRelation']>;
  unblockChatUser(blockedUserId: string): Promise<Schema['v1BlockRelation']>;
  prepareChatAttachment(value: Record<string, JsonValue>): Promise<Schema['v1AttachmentUpload']>;
  completeChatAttachment(attachmentId: string): Promise<ChatAttachment>;
  chatAttachmentDownload(attachmentId: string): Promise<Schema['v1AttachmentDownload']>;
  uploadChatAttachment(
    conversationId: string,
    file: File,
    onProgress?: (percent: number) => void,
  ): Promise<ChatAttachment>;
  reportChatMessage(messageId: string, reason: string): Promise<Schema['v1MessageReport']>;
  createChatTicket(): Promise<Schema['v1WebSocketTicket']>;
}
export const api: ApiClient;
export const API_BASE: string;
/**
 * Which cached views a server frame invalidates: the open thread, only the conversation list
 * (read receipts move unread counters), or nothing (typing, presence, command acks).
 */
export function chatFrameScope(frame: unknown): 'none' | 'conversations' | 'thread';
export class ChatSocket extends EventTarget {
  /** true while the underlying WebSocket is open; send() throws otherwise. */
  readonly connected: boolean;
  connect(): Promise<void>;
  send(frame: Record<string, JsonValue>): void;
  /** Best-effort variant used for presence commands (typing, read marks). */
  sendIfConnected(frame: Record<string, JsonValue>): void;
  sendMessage(
    conversationId: string,
    body: string,
    attachmentIds?: string[],
    kind?: string,
    replyToMessageId?: string,
  ): string;
  editMessage(messageId: string, body: string): void;
  withdrawMessage(messageId: string): void;
  /** Sends a receipt only when `sequence` advances past the last one sent; returns whether it did. */
  markRead(conversationId: string, sequence: number): boolean;
  typing(conversationId: string, typing: boolean): void;
  close(): void;
}

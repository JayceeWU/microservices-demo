import { Component, inject, OnDestroy, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import {
  api,
  chatFrameScope,
  ChatSocket,
  type ChatConversation,
  type ChatMessage,
} from '@dancehub/api-client';
import { messageOf, Workspace } from '../workspace';

@Component({
  standalone: true,
  imports: [FormsModule, CommonModule],
  template: `<section>
    <p class="eyebrow">Teacher messaging</p>
    <h1>Students & teacher groups</h1>
    <p>{{ connected() ? 'Connected' : 'Reconnecting…' }}</p>
    <form class="inline" (ngSubmit)="openStudent()">
      <input
        [(ngModel)]="studentId"
        name="studentId"
        placeholder="Student account ID"
        required
      /><button class="dh-button">Message student</button>
    </form>
    <form class="inline" (ngSubmit)="createGroup()">
      <input
        [(ngModel)]="groupTitle"
        name="groupTitle"
        placeholder="New teacher group"
        required
      /><button class="dh-button dh-button--primary">Create group</button>
    </form>
    <p class="error" *ngIf="error()">{{ error() }}</p>
    <div class="chat-layout">
      <aside>
        <button
          class="dh-choice"
          *ngFor="let c of conversations()"
          [attr.aria-pressed]="selectedId === c.id"
          (click)="select(c)"
        >
          {{ c.title || c.kind }} <span>{{ unread(c) }}</span>
        </button>
        <div class="dh-actions" *ngIf="selectedStudentId()">
          <button class="dh-button dh-button--danger" type="button" (click)="setBlock(true)">
            Block student
          </button>
          <button
            class="dh-button dh-button--ghost dh-button--inverse"
            type="button"
            (click)="setBlock(false)"
          >
            Unblock student
          </button>
        </div>
        <button
          class="dh-button dh-button--danger"
          *ngIf="selectedGroupId()"
          type="button"
          (click)="deleteGroup()"
        >
          Delete group
        </button>
      </aside>
      <div class="chat-thread">
        <article *ngFor="let message of messages().slice().reverse()">
          <small>{{ message.senderDisplayName || message.senderId }}</small>
          <p>{{ message.withdrawnAt ? 'Message withdrawn' : message.body }}</p>
          <div class="dh-actions">
            <button
              class="dh-button"
              *ngFor="let item of message.attachments"
              type="button"
              (click)="openAttachment(item.id)"
            >
              Open {{ item.kind === 'MESSAGE_KIND_VIDEO' ? 'video' : 'image' }}
            </button>
            <button
              class="dh-button dh-button--ghost"
              *ngIf="message.senderId === ws.profile()?.id && !message.withdrawnAt"
              type="button"
              (click)="edit(message)"
            >
              Edit
            </button>
            <button
              class="dh-button dh-button--danger"
              *ngIf="message.senderId === ws.profile()?.id && !message.withdrawnAt"
              type="button"
              (click)="withdraw(message)"
            >
              Withdraw
            </button>
          </div>
        </article>
        <form class="inline" (ngSubmit)="send()" *ngIf="selectedId">
          <input
            class="dh-file"
            type="file"
            accept="image/jpeg,image/png,image/webp,image/gif,video/mp4"
            (change)="upload($event)"
          />
          <span *ngIf="uploadStatus() === 'uploading'">{{ uploadProgress() }}%</span>
          <span *ngIf="uploadStatus() === 'scanning'">Security scan in progress…</span>
          <span *ngIf="uploadStatus() === 'ready'">Attachment ready</span>
          <input
            [(ngModel)]="body"
            name="body"
            maxlength="4000"
            placeholder="Write a message"
          /><button
            class="dh-button dh-button--primary"
            [disabled]="
              !connected() ||
              uploadStatus() === 'uploading' ||
              uploadStatus() === 'scanning' ||
              (!body.trim() && !attachmentId)
            "
          >
            Send
          </button>
        </form>
      </div>
    </div>
  </section>`,
})
export default class ChatPage implements OnDestroy {
  public ws = inject(Workspace);
  conversations = signal<ChatConversation[]>([]);
  messages = signal<ChatMessage[]>([]);
  connected = signal(false);
  error = signal('');
  studentId = '';
  groupTitle = '';
  selectedId = '';
  body = '';
  attachmentId = '';
  attachmentKind = '';
  uploadProgress = signal(0);
  uploadStatus = signal<'idle' | 'uploading' | 'scanning' | 'ready'>('idle');
  private socket = new ChatSocket();
  constructor() {
    this.socket.addEventListener('connected', () => this.connected.set(true));
    this.socket.addEventListener('disconnected', () => this.connected.set(false));
    this.socket.addEventListener('frame', (event) => {
      // Typing, presence and acks change nothing on screen; a read receipt only moves the
      // unread counters, so it must not reload the thread and re-send our own receipt.
      const scope = chatFrameScope((event as CustomEvent).detail);
      if (scope === 'thread') void this.refresh();
      else if (scope === 'conversations') void this.refreshConversations();
    });
    void this.socket.connect().catch((error: unknown) => this.error.set(messageOf(error)));
    void this.refresh();
  }
  ngOnDestroy() {
    this.socket.close();
  }
  async refreshConversations() {
    try {
      const value = await api.chatConversations();
      this.conversations.set(value.conversations || []);
    } catch (error: unknown) {
      this.error.set(messageOf(error));
    }
  }
  async refresh() {
    try {
      const value = await api.chatConversations();
      this.conversations.set(value.conversations || []);
      if (this.selectedId) {
        const messages = (await api.chatMessages(this.selectedId)).messages || [];
        this.messages.set(messages);
        const last = messages.reduce(
          (highest, message) => Math.max(highest, Number(message.sequence || 0)),
          0,
        );
        // The socket drops receipts that do not advance, so this is safe on every refresh.
        this.socket.markRead(this.selectedId, last);
      }
    } catch (error: unknown) {
      this.error.set(messageOf(error));
    }
  }
  async openStudent() {
    const value = await api.createDirectConversation(this.studentId);
    this.selectedId = value.id || '';
    await this.refresh();
  }
  async createGroup() {
    const value = await api.createChatGroup({
      kind: 4,
      studioId: this.ws.studioId(),
      title: this.groupTitle,
    });
    this.selectedId = value.id || '';
    await this.refresh();
  }
  async select(value: ChatConversation) {
    this.selectedId = value.id || '';
    await this.refresh();
  }
  async openAttachment(id?: string) {
    if (!id) return;
    const value = await api.chatAttachmentDownload(id);
    if (value.downloadUrl) window.open(value.downloadUrl, '_blank', 'noopener');
  }
  edit(message: ChatMessage) {
    const body = window.prompt('Edit message', message.body || '');
    if (body && message.id) this.socket.editMessage(message.id, body);
  }
  withdraw(message: ChatMessage) {
    if (message.id) this.socket.withdrawMessage(message.id);
  }
  unread(value: ChatConversation) {
    return Math.max(0, Number(value.lastSequence || 0) - Number(value.lastReadSequence || 0));
  }
  selectedStudentId() {
    const selected = this.conversations().find(
      (conversation) => conversation.id === this.selectedId,
    );
    return selected?.kind === 'CONVERSATION_KIND_DIRECT_TEACHER' ? selected.studentId || '' : '';
  }
  selectedGroupId() {
    const selected = this.conversations().find(
      (conversation) => conversation.id === this.selectedId,
    );
    return selected?.kind === 'CONVERSATION_KIND_TEACHER_GROUP' ? selected.id || '' : '';
  }
  async deleteGroup() {
    const groupId = this.selectedGroupId();
    if (!groupId) return;
    const reason = window.prompt('Audit reason for deleting this group');
    if (!reason?.trim()) return;
    try {
      await api.deleteChatGroup(groupId, reason.trim());
      this.selectedId = '';
      await this.refresh();
    } catch (error: unknown) {
      this.error.set(messageOf(error));
    }
  }
  async setBlock(active: boolean) {
    const studentId = this.selectedStudentId();
    if (!studentId) return;
    try {
      if (active) await api.blockChatUser(studentId);
      else await api.unblockChatUser(studentId);
    } catch (error: unknown) {
      this.error.set(messageOf(error));
    }
  }
  async upload(event: Event) {
    const file = (event.target as HTMLInputElement).files?.[0];
    if (!file) return;
    this.uploadStatus.set('uploading');
    try {
      const value = await api.uploadChatAttachment(this.selectedId, file, (progress) => {
        this.uploadProgress.set(progress);
        if (progress === 100) this.uploadStatus.set('scanning');
      });
      this.attachmentId = value.id || '';
      this.attachmentKind = file.type === 'video/mp4' ? 'MESSAGE_KIND_VIDEO' : 'MESSAGE_KIND_IMAGE';
      this.uploadStatus.set('ready');
    } catch (error: unknown) {
      this.uploadProgress.set(0);
      this.uploadStatus.set('idle');
      this.error.set(messageOf(error));
    }
  }
  send() {
    if (!this.selectedId || (!this.body.trim() && !this.attachmentId)) return;
    try {
      this.socket.sendMessage(
        this.selectedId,
        this.body.trim(),
        this.attachmentId ? [this.attachmentId] : [],
        this.attachmentKind,
      );
    } catch (error: unknown) {
      // Keep the draft: the socket is reconnecting and the message was not delivered.
      this.error.set(messageOf(error));
      return;
    }
    this.error.set('');
    this.body = '';
    this.attachmentId = '';
    this.uploadProgress.set(0);
    this.uploadStatus.set('idle');
  }
}

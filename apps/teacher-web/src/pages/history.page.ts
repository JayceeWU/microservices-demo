import { Component, inject, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { api, type ClassSession } from '@dancehub/api-client';
import { loadTeacherSessions, messageOf, Workspace } from '../workspace';

@Component({
  standalone: true,
  imports: [FormsModule, CommonModule],
  template: `<section>
    <p class="eyebrow">Teacher workspace</p>
    <h1>History & videos</h1>
    <p>Completed classes and their externally hosted lesson videos.</p>
    <form class="inline" (ngSubmit)="save()">
      <input
        [(ngModel)]="sessionId"
        name="sessionId"
        placeholder="Class session ID"
        required
      /><input
        [(ngModel)]="videoUrl"
        name="videoUrl"
        type="url"
        placeholder="https://…"
        required
      /><button class="dh-button dh-button--primary">Save video link</button>
    </form>
    <p class="error" *ngIf="error()">{{ error() }}</p>
    <p class="notice" *ngIf="message()">{{ message() }}</p>
    <div class="schedule history-schedule">
      <article *ngFor="let s of sessions()">
        <div>
          <p class="pill">{{ s.status }}</p>
          <h3>{{ s.title }}</h3>
          <p>{{ s.startsAt | date: 'medium' }}</p>
          <a
            class="dh-button"
            *ngIf="s.videoUrl"
            [href]="s.videoUrl"
            target="_blank"
            rel="noopener noreferrer"
            >Open lesson video</a
          ><span *ngIf="!s.videoUrl">No video link yet.</span>
        </div>
      </article>
    </div>
    <div class="empty" *ngIf="!sessions().length && !error()">No completed classes yet.</div>
  </section>`,
})
export default class HistoryPage {
  public ws = inject(Workspace);
  sessionId = '';
  videoUrl = '';
  sessions = signal<ClassSession[]>([]);
  error = signal('');
  message = signal('');
  private generation = 0;
  constructor() {
    this.load();
  }
  async load() {
    const generation = ++this.generation;
    try {
      const sessions = await loadTeacherSessions(this.ws);
      if (generation !== this.generation) return;
      this.sessions.set(
        sessions
          .filter((session) => session.status === 'COMPLETED')
          .sort((a, b) => b.startsAt.localeCompare(a.startsAt)),
      );
      this.error.set('');
    } catch (error: unknown) {
      if (generation === this.generation) this.error.set(messageOf(error));
    }
  }
  async save() {
    try {
      await api.updateClassVideo(
        this.sessionId,
        this.videoUrl,
        'Teacher updated external lesson video',
      );
      this.message.set('Video link saved.');
      await this.load();
    } catch (error: unknown) {
      this.error.set(messageOf(error));
    }
  }
}

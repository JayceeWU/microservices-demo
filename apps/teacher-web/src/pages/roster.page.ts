import { Component, inject, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute } from '@angular/router';
import { api, type Booking } from '@dancehub/api-client';
import { messageOf } from '../workspace';

@Component({
  standalone: true,
  imports: [FormsModule, CommonModule],
  template: `<section>
    <p class="eyebrow">Teacher workspace</p>
    <h1>Roster & attendance</h1>
    <form class="inline" (ngSubmit)="load()">
      <input
        [(ngModel)]="sessionId"
        name="sessionId"
        placeholder="Class session ID"
        required
      /><button class="dh-button" [disabled]="loading()">Load roster</button>
    </form>
    <p class="error" *ngIf="error()">{{ error() }}</p>
    <div class="schedule roster-schedule">
      <article *ngFor="let b of bookings()">
        <div>
          <h3>{{ b.studentName || b.studentId }}</h3>
          <p>{{ b.status }}</p>
        </div>
        <div class="dh-actions">
          <button
            class="dh-button dh-button--primary"
            [disabled]="saving()"
            (click)="attend(b, true)"
          >
            Attended
          </button>
          <button
            class="dh-button dh-button--danger"
            [disabled]="saving()"
            (click)="attend(b, false)"
          >
            No show
          </button>
        </div>
      </article>
    </div>
    <button class="dh-button" *ngIf="nextPageToken()" [disabled]="loading()" (click)="loadMore()">
      Load more bookings
    </button>
  </section>`,
})
export default class RosterPage {
  private route = inject(ActivatedRoute);
  sessionId = '';
  bookings = signal<Booking[]>([]);
  error = signal('');
  loading = signal(false);
  saving = signal(false);
  nextPageToken = signal('');
  private loadedSession = '';
  private generation = 0;
  private attendanceCommands = new Map<string, { attended: boolean; key: string }>();
  constructor() {
    this.route.queryParamMap.subscribe((params) => {
      const session = params.get('session');
      if (session) {
        this.sessionId = session;
        void this.load();
      }
    });
  }
  async load() {
    this.loadedSession = this.sessionId;
    this.bookings.set([]);
    this.nextPageToken.set('');
    await this.loadPage('', ++this.generation);
  }
  async loadMore() {
    if (this.loading() || !this.nextPageToken()) return;
    await this.loadPage(this.nextPageToken(), this.generation);
  }
  private async loadPage(pageToken: string, generation: number) {
    this.loading.set(true);
    try {
      const value = await api.roster(this.loadedSession, { page_token: pageToken });
      if (generation !== this.generation) return;
      const bookings = value.bookings || value.roster || [];
      this.bookings.update((current) => (pageToken ? [...current, ...bookings] : bookings));
      this.nextPageToken.set(value.nextPageToken || '');
      this.error.set('');
    } catch (error: unknown) {
      if (generation === this.generation) this.error.set(messageOf(error));
    } finally {
      if (generation === this.generation) this.loading.set(false);
    }
  }
  async attend(booking: Booking, value: boolean) {
    if (!booking.id || this.saving()) return;
    let command = this.attendanceCommands.get(booking.id);
    if (!command || command.attended !== value) {
      command = { attended: value, key: crypto.randomUUID() };
      this.attendanceCommands.set(booking.id, command);
    }
    this.saving.set(true);
    try {
      await api.correctAttendance(
        booking.id,
        value,
        'Teacher attendance confirmation',
        command.key,
      );
      this.attendanceCommands.delete(booking.id);
      await this.load();
    } catch (error: unknown) {
      this.error.set(messageOf(error));
    } finally {
      this.saving.set(false);
    }
  }
}

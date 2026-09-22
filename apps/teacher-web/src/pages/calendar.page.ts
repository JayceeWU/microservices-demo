import { Component, inject, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterLink } from '@angular/router';
import type { ClassSession } from '@dancehub/api-client';
import { loadTeacherSessions, messageOf, Workspace } from '../workspace';

@Component({
  standalone: true,
  selector: 'teacher-calendar',
  imports: [CommonModule, RouterLink],
  template: `<section>
    <p class="eyebrow">Teacher workspace</p>
    <h1>Cross-studio calendar</h1>
    <p>Your personal calendar aggregates every studio membership.</p>
    <div class="page-actions dh-actions">
      <button class="dh-button" (click)="load()">Refresh</button>
    </div>
    <p class="error" *ngIf="error()">{{ error() }}</p>
    <div class="schedule">
      <article *ngFor="let s of sessions(); trackBy: track">
        <time
          ><strong>{{ s.startsAt | date: 'dd' }}</strong
          ><span>{{ s.startsAt | date: 'MMM' }}</span></time
        >
        <div>
          <p class="pill">{{ s.status }}</p>
          <h3>{{ s.title }}</h3>
          <p>
            {{ s.startsAt | date: 'EEE, h:mm a' }} · {{ s.confirmedCount }}/{{ s.capacity }} dancers
          </p>
        </div>
        <a class="dh-button" [routerLink]="['/roster']" [queryParams]="{ session: s.id }">Roster</a>
      </article>
    </div>
    <div class="empty" *ngIf="!sessions().length && !error()">No scheduled classes.</div>
  </section>`,
})
export default class CalendarPage {
  public ws = inject(Workspace);
  sessions = signal<ClassSession[]>([]);
  error = signal('');
  private generation = 0;
  constructor() {
    this.load();
  }
  track(_: number, value: ClassSession) {
    return value.id;
  }
  async load() {
    const generation = ++this.generation;
    try {
      const sessions = await loadTeacherSessions(this.ws);
      if (generation !== this.generation) return;
      this.sessions.set(sessions.sort((a, b) => a.startsAt.localeCompare(b.startsAt)));
      this.error.set('');
    } catch (error: unknown) {
      if (generation === this.generation) this.error.set(messageOf(error));
    }
  }
}

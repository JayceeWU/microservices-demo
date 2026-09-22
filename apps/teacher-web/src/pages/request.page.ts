import { Component, inject, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { api } from '@dancehub/api-client';
import { messageOf, type ScheduleRequest, Workspace } from '../workspace';

@Component({
  standalone: true,
  imports: [FormsModule, CommonModule],
  template: `<section>
    <p class="eyebrow">Teacher workspace</p>
    <h1>Request a class</h1>
    <form (ngSubmit)="submit()">
      <label>Title<input [(ngModel)]="form.title" name="title" required /></label
      ><label>Room ID<input [(ngModel)]="form.roomId" name="roomId" required /></label
      ><label
        >Starts<input
          [(ngModel)]="form.startsAt"
          name="startsAt"
          type="datetime-local"
          required /></label
      ><label
        >Ends<input [(ngModel)]="form.endsAt" name="endsAt" type="datetime-local" required /></label
      ><label
        >Capacity<input [(ngModel)]="form.capacity" name="capacity" type="number" min="1" /></label
      ><button class="dh-button dh-button--primary">Submit for approval</button>
    </form>
    <p class="notice" *ngIf="message()">{{ message() }}</p>
  </section>`,
})
export default class RequestPage {
  public ws = inject(Workspace);
  form: ScheduleRequest = { title: '', roomId: '', startsAt: '', endsAt: '', capacity: 20 };
  message = signal('');
  async submit() {
    try {
      await api.submitSchedule({
        ...this.form,
        studioId: this.ws.studioId(),
        startsAt: new Date(this.form.startsAt).toISOString(),
        endsAt: new Date(this.form.endsAt).toISOString(),
      });
      this.message.set('Schedule request submitted.');
    } catch (error: unknown) {
      this.message.set(messageOf(error));
    }
  }
}

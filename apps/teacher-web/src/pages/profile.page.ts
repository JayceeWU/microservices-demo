import { Component, inject, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { api, type Profile } from '@dancehub/api-client';
import { messageOf, Workspace } from '../workspace';

@Component({
  standalone: true,
  imports: [FormsModule, CommonModule],
  template: `<section>
    <p class="eyebrow">Teacher workspace</p>
    <h1>Teacher profile</h1>
    <form *ngIf="ws.profile() as p" (ngSubmit)="save()">
      <label
        >Display name<input [(ngModel)]="model.displayName" name="displayName" required /></label
      ><label>Timezone<input [(ngModel)]="model.timezone" name="timezone" required /></label
      ><label>Bio<textarea [(ngModel)]="model.bio" name="bio"></textarea></label
      ><label>Portfolio URL<input [(ngModel)]="model.portfolioUrl" name="portfolioUrl" /></label
      ><button class="dh-button dh-button--primary">Save</button>
    </form>
    <p class="notice" *ngIf="message()">{{ message() }}</p>
  </section>`,
})
export default class ProfilePage {
  public ws = inject(Workspace);
  model: Partial<Profile> = { ...(this.ws.profile() || {}) };
  message = signal('');
  async save() {
    try {
      this.ws.profile.set(await api.updateProfile(this.model));
      this.message.set('Profile saved.');
    } catch (error: unknown) {
      this.message.set(messageOf(error));
    }
  }
}

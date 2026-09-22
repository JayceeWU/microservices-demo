import { Component, inject } from '@angular/core';
import 'zone.js';
import { bootstrapApplication } from '@angular/platform-browser';
import {
  provideRouter,
  RouterLink,
  RouterLinkActive,
  RouterOutlet,
  type Routes,
} from '@angular/router';
import { CommonModule } from '@angular/common';
import { initializeApiClient, setAuthContext } from '@dancehub/api-client';
import { createOidcClient } from '@dancehub/auth';
import { initializeBrowserTelemetry } from '@dancehub/telemetry';
import { Workspace } from './workspace';

const runtime = globalThis as typeof globalThis & {
  DANCEHUB_OIDC_ISSUER?: string;
  DANCEHUB_AUTH_MODE?: string;
};
const routes: Routes = [
  { path: '', loadComponent: () => import('./pages/calendar.page').then((page) => page.default) },
  {
    path: 'request',
    loadComponent: () => import('./pages/request.page').then((page) => page.default),
  },
  {
    path: 'roster',
    loadComponent: () => import('./pages/roster.page').then((page) => page.default),
  },
  {
    path: 'history',
    loadComponent: () => import('./pages/history.page').then((page) => page.default),
  },
  {
    path: 'profile',
    loadComponent: () => import('./pages/profile.page').then((page) => page.default),
  },
  { path: 'chat', loadComponent: () => import('./pages/chat.page').then((page) => page.default) },
  { path: '**', redirectTo: '' },
];
initializeApiClient(globalThis.DANCEHUB_API_URL);
const oidc = createOidcClient({
  issuer: runtime.DANCEHUB_OIDC_ISSUER,
  clientId: 'teacher-web',
  onToken: (accessToken) => setAuthContext({ accessToken, userId: '' }),
});

@Component({
  selector: 'dancehub-teacher',
  standalone: true,
  imports: [CommonModule, RouterLink, RouterLinkActive, RouterOutlet],
  template: `<header>
      <a class="brand" routerLink="/">BayArea<span>Dance</span>Hub</a>
      <strong>Teacher Studio</strong>
      <div class="header-controls dh-actions">
        <select
          class="dh-select"
          aria-label="Current studio"
          [value]="ws.studioId()"
          (change)="changeStudio($event)"
        >
          <option value="">Select studio</option>
          <option *ngFor="let m of ws.memberships()" [value]="m.studioId">
            {{ m.studioName || m.studioId }}
          </option>
        </select>
        <button class="dh-button dh-button--ghost" type="button" (click)="logout()">
          Sign out
        </button>
      </div>
    </header>
    <main *ngIf="ws.ready(); else loading">
      <aside>
        <div class="avatar">{{ initials() }}</div>
        <h2>{{ ws.profile()?.displayName || 'Teacher' }}</h2>
        <nav>
          <a routerLink="/" routerLinkActive="active" [routerLinkActiveOptions]="{ exact: true }"
            >Calendar</a
          ><a routerLink="/request" routerLinkActive="active">Schedule request</a
          ><a routerLink="/roster" routerLinkActive="active">Rosters</a
          ><a routerLink="/history" routerLinkActive="active">History & videos</a
          ><a routerLink="/profile" routerLinkActive="active">Profile</a>
          <a routerLink="/chat" routerLinkActive="active">Messages & groups</a>
        </nav>
      </aside>
      <div class="content">
        <div class="error" *ngIf="ws.error()">
          <h1>Sign in required</h1>
          <p>{{ ws.error() }}</p>
          <button class="dh-button dh-button--primary" type="button" (click)="login()">
            Sign in with Keycloak
          </button>
        </div>
        <router-outlet *ngIf="!ws.error()" />
      </div>
    </main>
    <ng-template #loading><p class="loading">Loading teacher workspace…</p></ng-template>`,
})
class App {
  public ws = inject(Workspace);
  constructor() {
    this.ws.load();
  }
  initials() {
    return (this.ws.profile()?.displayName || 'T')
      .split(' ')
      .map((v: string) => v[0])
      .join('')
      .slice(0, 2);
  }
  changeStudio(event: Event) {
    this.ws.changeStudio((event.target as HTMLSelectElement).value);
  }
  login() {
    void oidc.login();
  }
  logout() {
    oidc.logout();
  }
}

async function start() {
  initializeBrowserTelemetry('teacher-web');
  if (runtime.DANCEHUB_AUTH_MODE !== 'dev') {
    try {
      const session = await oidc.initialize();
      if (!session) await oidc.login();
    } catch (error) {
      console.error(error);
      return;
    }
  }
  await bootstrapApplication(App, { providers: [provideRouter(routes)] });
}
void start().catch(console.error);

import { Injectable, signal } from '@angular/core';
import {
  api,
  collectPages,
  getAuthContext,
  selectStudio,
  type ClassSession,
  type Membership,
  type Profile,
} from '@dancehub/api-client';
import { activeTeacherMemberships, initialStudio } from './workspace-policy.js';

export const messageOf = (error: unknown) =>
  error instanceof Error ? error.message : 'Unexpected error';
export type ScheduleRequest = {
  title: string;
  roomId: string;
  startsAt: string;
  endsAt: string;
  capacity: number;
};

@Injectable({ providedIn: 'root' })
export class Workspace {
  profile = signal<Profile | null>(null);
  memberships = signal<Membership[]>([]);
  studioId = signal('');
  error = signal('');
  ready = signal(false);
  async load() {
    try {
      const [profile, memberships] = await Promise.all([api.profile(), api.memberships()]);
      this.profile.set(profile);
      const allowed = activeTeacherMemberships(memberships.memberships);
      this.memberships.set(allowed);
      const current = getAuthContext().studioId;
      this.changeStudio(initialStudio(allowed, current));
    } catch (error: unknown) {
      this.error.set(messageOf(error));
    } finally {
      this.ready.set(true);
    }
  }
  changeStudio(id: string) {
    this.studioId.set(id);
    if (id) selectStudio(id);
  }
}

export async function loadTeacherSessions(workspace: Workspace): Promise<ClassSession[]> {
  if (!workspace.ready()) await workspace.load();
  const teacherId = workspace.profile()?.id || '';
  const studioIds = [
    ...new Set(
      workspace
        .memberships()
        .map((membership) => membership.studioId)
        .filter((id): id is string => Boolean(id)),
    ),
  ];
  const results = await Promise.all(
    studioIds.map((studioId) =>
      collectPages<ClassSession>(async (pageToken) => {
        const page = await api.sessions({
          studio_id: studioId,
          teacher_id: teacherId,
          page_token: pageToken,
        });
        return { items: page.sessions, nextPageToken: page.nextPageToken };
      }),
    ),
  );
  return results.flat();
}

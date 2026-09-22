'use client';
import type { FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, type Profile } from '@dancehub/api-client';
import { ErrorBox, Page } from '../workspace-shared';

export default function ProfilePage({ notice }: { notice: (s: string) => void }) {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ['profile'], queryFn: api.profile });
  const save = useMutation({
    mutationFn: (value: Partial<Profile>) => api.updateProfile(value),
    onSuccess: () => {
      notice('Profile saved');
      qc.invalidateQueries({ queryKey: ['profile'] });
    },
  });
  if (q.isLoading)
    return (
      <Page title="Profile" subtitle="">
        Loading…
      </Page>
    );
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const values = Object.fromEntries(new FormData(event.currentTarget)) as Record<string, string>;
    save.mutate(values);
  };
  return (
    <Page title="Profile" subtitle="Your public and account details">
      <ErrorBox error={q.error || save.error} />
      <form className="form" onSubmit={submit}>
        <label>
          Display name
          <input name="displayName" defaultValue={q.data?.displayName} required />
        </label>
        <label>
          Timezone
          <input name="timezone" defaultValue={q.data?.timezone} required />
        </label>
        <label>
          Bio
          <textarea name="bio" defaultValue={q.data?.bio} />
        </label>
        <label>
          Portfolio URL
          <input name="portfolioUrl" defaultValue={q.data?.portfolioUrl} />
        </label>
        <button className="dh-button dh-button--primary">Save</button>
      </form>
    </Page>
  );
}

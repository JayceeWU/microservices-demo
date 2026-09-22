'use client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, type Teacher } from '@dancehub/api-client';
import { useRouter } from 'next/navigation';
import { ErrorBox, Page } from '../workspace-shared';

export default function Teachers({
  studioId,
  notice,
}: {
  studioId: string;
  notice: (value: string) => void;
}) {
  const qc = useQueryClient();
  const router = useRouter();
  const teachers = useQuery({
    queryKey: ['teachers', studioId],
    queryFn: () => api.teachers({ studio_id: studioId }),
    enabled: !!studioId,
  });
  const contact = useMutation({
    mutationFn: (teacherId: string) => api.createDirectConversation(teacherId),
    onSuccess: (value) => {
      notice('Teacher conversation opened');
      qc.invalidateQueries({ queryKey: ['chat-conversations'] });
      if (value.id) router.push(`/chat?conversation=${value.id}`);
    },
  });
  return (
    <Page title="Teachers" subtitle="Profiles and public teacher groups">
      <ErrorBox error={teachers.error || contact.error} />
      <div className="grid">
        {teachers.data?.teachers?.map((teacher: Teacher) => (
          <article className="card teacher" key={teacher.id}>
            <h3>{teacher.displayName}</h3>
            <p>{teacher.bio}</p>
            <div className="card-actions dh-actions">
              <button
                className="dh-button"
                type="button"
                onClick={() => teacher.id && contact.mutate(teacher.id)}
              >
                Contact teacher
              </button>
              <a className="dh-button dh-button--ghost" href={`/chat?teacher_id=${teacher.id}`}>
                View teacher groups
              </a>
            </div>
          </article>
        ))}
      </div>
    </Page>
  );
}

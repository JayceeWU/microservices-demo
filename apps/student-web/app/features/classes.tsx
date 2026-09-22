'use client';
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api, type ClassSession } from '@dancehub/api-client';
import { Empty, ErrorBox, Page } from '../workspace-shared';

export default function Classes({
  studioId,
  notice,
}: {
  studioId: string;
  notice: (s: string) => void;
}) {
  const qc = useQueryClient();
  const q = useInfiniteQuery({
    queryKey: ['sessions', studioId, 'bookable'],
    initialPageParam: '',
    queryFn: ({ pageParam }) =>
      api.sessions({ studio_id: studioId, bookable_only: true, page_token: pageParam }),
    getNextPageParam: (page) => page.nextPageToken || undefined,
    enabled: !!studioId,
  });
  const book = useMutation({
    mutationFn: (id: string) => api.bookClass(id),
    onSuccess: () => {
      notice('Class booked');
      qc.invalidateQueries({ queryKey: ['bookings'] });
      qc.invalidateQueries({ queryKey: ['sessions'] });
    },
  });
  return (
    <Page title="Classes" subtitle="Open classes at the selected studio">
      <ErrorBox error={q.error || book.error} />
      <div className="grid">
        {q.data?.pages
          .flatMap((page) => page.sessions)
          .map((session: ClassSession) => (
            <article className="card" key={session.id}>
              <p className="pill">{session.status}</p>
              <h3>{session.title}</h3>
              <p>
                {new Date(session.startsAt).toLocaleString()} · {session.creditCost} credits
              </p>
              <p>
                {session.confirmedCount}/{session.capacity} dancers
              </p>
              <button
                className="dh-button dh-button--primary"
                disabled={book.isPending || !session.id}
                onClick={() => session.id && book.mutate(session.id)}
              >
                Book
              </button>
            </article>
          ))}
      </div>
      {q.hasNextPage && (
        <button
          className="dh-button"
          disabled={q.isFetching}
          onClick={() => q.fetchNextPage({ cancelRefetch: false })}
        >
          Load more classes
        </button>
      )}
      {q.data && !q.data.pages.some((page) => page.sessions.length) && <Empty name="classes" />}
    </Page>
  );
}

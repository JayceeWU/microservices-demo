'use client';
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api, type Booking } from '@dancehub/api-client';
import { Empty, ErrorBox, Page } from '../workspace-shared';

export default function Bookings({ notice }: { notice: (s: string) => void }) {
  const qc = useQueryClient();
  const q = useInfiniteQuery({
    queryKey: ['bookings'],
    initialPageParam: '',
    queryFn: ({ pageParam }) => api.myBookings({ page_token: pageParam }),
    getNextPageParam: (page) => page.nextPageToken || undefined,
    refetchInterval: (query) =>
      query.state.data?.pages.some((page) =>
        page.bookings.some((booking) => booking.creditCompensationPending),
      )
        ? 2000
        : false,
  });
  const cancel = useMutation({
    mutationFn: (id: string) => api.cancelBooking(id, 'Student cancellation'),
    onSuccess: () => {
      notice('Booking cancelled');
      qc.invalidateQueries({ queryKey: ['bookings'] });
      qc.invalidateQueries({ queryKey: ['sessions'] });
    },
  });
  return (
    <Page title="My bookings" subtitle="Cancellation closes four hours before class">
      <ErrorBox error={q.error || cancel.error} />
      <div className="list">
        {q.data?.pages
          .flatMap((page) => page.bookings)
          .map((booking: Booking) => (
            <article className="row" key={booking.id}>
              <div>
                <b>{booking.title || booking.classSessionId}</b>
                <p>{booking.status}</p>
                {booking.creditCompensationPending && (
                  <p role="status">Cancelled. Your credits are being returned automatically.</p>
                )}
              </div>
              {booking.id && ['CONFIRMED', 'PENDING_CREDIT'].includes(booking.status) && (
                <button
                  className="dh-button dh-button--danger"
                  disabled={cancel.isPending}
                  onClick={() => cancel.mutate(booking.id!)}
                >
                  Cancel
                </button>
              )}
            </article>
          ))}
      </div>
      {q.hasNextPage && (
        <button
          className="dh-button"
          disabled={q.isFetching}
          onClick={() => q.fetchNextPage({ cancelRefetch: false })}
        >
          Load more bookings
        </button>
      )}
      {q.data && !q.data.pages.some((page) => page.bookings.length) && <Empty name="bookings" />}
    </Page>
  );
}

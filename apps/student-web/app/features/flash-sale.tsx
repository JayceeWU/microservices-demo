'use client';
import { useRouter, useSearchParams } from 'next/navigation';
import { useMutation, useQuery } from '@tanstack/react-query';
import { api, ApiError } from '@dancehub/api-client';
import { ErrorBox, Page } from '../workspace-shared';
import { checkoutKey } from '../checkout-key';

export default function FlashSale({ notice }: { notice: (s: string) => void }) {
  const router = useRouter();
  const requestId = useSearchParams().get('requestId') || '';
  const reserve = useMutation({
    mutationFn: (id: string) => api.reserveFlashSale(id, checkoutKey(`flash-${id}`, id)),
    onSuccess: (value) => {
      router.push(`/flash-sale?requestId=${encodeURIComponent(value.requestId)}`);
      notice('Request accepted; polling queue');
    },
  });
  const q = useQuery({
    queryKey: ['flash', requestId],
    queryFn: async () => {
      try {
        return await api.flashSaleRequest(requestId);
      } catch (error) {
        // The accepted Redis request may not yet have reached the order database.
        if (error instanceof ApiError && error.status === 404)
          return { requestId, campaignId: '', status: 'QUEUED', orderId: '', reason: '' };
        throw error;
      }
    },
    enabled: !!requestId,
    refetchInterval: (query) =>
      ['ORDER_CREATED', 'REJECTED', 'EXPIRED'].includes(query.state.data?.status || '')
        ? false
        : 1500,
  });
  return (
    <Page title="Flash sale" subtitle="Admission is asynchronous and idempotent">
      <form
        className="inline"
        onSubmit={(e) => {
          e.preventDefault();
          reserve.mutate(String(new FormData(e.currentTarget).get('campaignId')));
        }}
      >
        <input name="campaignId" aria-label="Campaign ID" placeholder="Campaign ID" required />
        <button className="dh-button dh-button--primary" disabled={reserve.isPending}>
          Join queue
        </button>
      </form>
      <ErrorBox error={reserve.error || q.error} />
      {q.data && (
        <div className="queue" role="status">
          <b>{q.data.status}</b>
          <p>Request {requestId}</p>
          {q.data.reason && <p>{q.data.reason}</p>}
          {q.data.orderId && (
            <a
              className="dh-button dh-button--primary"
              href={`/orders?orderId=${encodeURIComponent(q.data.orderId)}`}
            >
              Continue to payment
            </a>
          )}
        </div>
      )}
    </Page>
  );
}

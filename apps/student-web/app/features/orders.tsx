'use client';
import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useRouter, useSearchParams } from 'next/navigation';
import { api, ApiError, type OrderStatus } from '@dancehub/api-client';
import { ErrorBox, Page, usd } from '../workspace-shared';
import { checkoutKey } from '../checkout-key';

const descriptions: Record<OrderStatus, string> = {
  DRAFT: 'Your order is being prepared.',
  PENDING_PAYMENT: 'Your order is ready for payment.',
  PAYMENT_FAILED: 'Payment failed. You can retry before the payment deadline.',
  PAID: 'Payment received. We are completing your purchase.',
  PAID_NOT_FULFILLED:
    'Payment received. Your passes or reservation are being prepared; this page updates automatically.',
  FULFILLED: 'Purchase complete. Your passes or room reservation are ready.',
  EXPIRED: 'The payment deadline has passed. Start a new order to purchase.',
  REFUND_PENDING: 'Your refund is being processed. This page updates automatically.',
  REFUNDED: 'Refund complete. The associated passes or reservation have been released.',
};
const label = (value: string) => value.toLowerCase().replaceAll('_', ' ');

export default function Orders() {
  const router = useRouter();
  const id = useSearchParams().get('orderId') || '';
  return (
    <Page title="Orders" subtitle="Review your purchase, complete payment and track refunds">
      <form
        className="inline"
        onSubmit={(event) => {
          event.preventDefault();
          const value = String(new FormData(event.currentTarget).get('id')).trim();
          router.push(`/orders?orderId=${encodeURIComponent(value)}`);
        }}
      >
        <input
          key={id}
          name="id"
          aria-label="Order ID"
          placeholder="Order ID"
          defaultValue={id}
          required
        />
        <button className="dh-button">Find order</button>
      </form>
      {id ? (
        <OrderDetail key={id} id={id} />
      ) : (
        <p className="empty">Complete checkout from your cart, or enter an order ID to continue.</p>
      )}
    </Page>
  );
}

function OrderDetail({ id }: { id: string }) {
  const qc = useQueryClient();
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);
  const order = useQuery({
    queryKey: ['order', id],
    queryFn: () => api.order(id),
    refetchInterval: (q) =>
      ['FULFILLED', 'REFUNDED', 'EXPIRED'].includes(q.state.data?.status || '') ? false : 2000,
  });
  const payment = useQuery({
    queryKey: ['order-payment', id],
    enabled: !!order.data,
    queryFn: async () => {
      try {
        return await api.paymentForOrder(id);
      } catch (error) {
        if (error instanceof ApiError && error.status === 404) return null;
        throw error;
      }
    },
    refetchInterval: (q) =>
      order.data?.status === 'REFUND_PENDING' ||
      (order.data?.status === 'REFUNDED' && q.state.data?.status !== 'REFUNDED') ||
      (q.state.data && !['SUCCEEDED', 'REFUNDED'].includes(q.state.data.status || ''))
        ? 2000
        : false,
  });
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ['order', id] });
    qc.invalidateQueries({ queryKey: ['order-payment', id] });
    qc.invalidateQueries({ queryKey: ['grants'] });
    qc.invalidateQueries({ queryKey: ['balances'] });
  };
  const create = useMutation({
    mutationFn: () => api.createPayment(id, checkoutKey(`payment-${id}`, id)),
    onSuccess: (value) => {
      qc.setQueryData(['order-payment', id], value);
      refresh();
    },
  });
  const simulate = useMutation({
    mutationFn: (outcome: 'SUCCEEDED' | 'FAILED') =>
      api.simulatePayment(
        payment.data!.id,
        outcome,
        checkoutKey(`simulate-${payment.data!.id}-${outcome}`, outcome),
      ),
    onSuccess: (value) => {
      qc.setQueryData(['order-payment', id], value);
      refresh();
    },
  });
  const refund = useMutation({
    mutationFn: () => {
      const room = order.data?.lines.find((line) => line.type === 'ROOM_RESERVATION');
      return room?.type === 'ROOM_RESERVATION'
        ? api.cancelRoomReservation(room.roomReservationId, 'Student requested room cancellation')
        : api.requestRefund(id, 'Student requested a full refund');
    },
    onSuccess: refresh,
  });
  const data = order.data;
  const expired = !!data?.paymentExpiresAt && Date.parse(data.paymentExpiresAt) <= now;
  const payable = !!data && ['PENDING_PAYMENT', 'PAYMENT_FAILED'].includes(data.status) && !expired;
  const canSimulate =
    payable &&
    payment.data?.simulationEnabled &&
    !['SUCCEEDED', 'REFUNDED'].includes(payment.data.status || '');
  return (
    <>
      <ErrorBox
        error={order.error || payment.error || create.error || simulate.error || refund.error}
      />
      {order.isPending && <p role="status">Loading your order…</p>}
      {data && (
        <div className="checkout-grid">
          <article className="card checkout-summary">
            <p className="pill">Order summary</p>
            <h2>Your purchase</h2>
            <p className="checkout-id">Order {data.id}</p>
            <div className="list">
              {data.lines.map((line) => (
                <div className="checkout-line" key={line.id}>
                  <div>
                    <b>
                      {line.description ||
                        (line.type === 'ROOM_RESERVATION' ? 'Studio room' : 'Dance pass')}
                    </b>
                    <p>
                      {line.type === 'CREDIT_PRODUCT'
                        ? `Quantity: ${line.quantity}${line.finalSale ? ' · Final sale' : ''}`
                        : 'Room reservation'}
                    </p>
                  </div>
                  <strong>
                    {usd.format(
                      (line.unitAmountCents *
                        (line.type === 'CREDIT_PRODUCT' ? line.quantity : 1)) /
                        100,
                    )}
                  </strong>
                </div>
              ))}
            </div>
            <div className="checkout-total">
              <span>Total</span>
              <strong>{usd.format(data.totalAmountCents / 100)}</strong>
            </div>
            {data.paymentExpiresAt && (
              <p>
                Payment deadline:{' '}
                <time dateTime={data.paymentExpiresAt}>
                  {new Date(data.paymentExpiresAt).toLocaleString()}
                </time>
              </p>
            )}
          </article>
          <article className="card checkout-payment">
            <p className="pill">Payment & progress</p>
            <h2 className="checkout-state">{label(data.status)}</h2>
            <p role="status">{descriptions[data.status]}</p>
            {data.refundPhase && (
              <p>
                Refund stage: <b>{label(data.refundPhase)}</b>
              </p>
            )}
            {data.refundFailureReason && <p>{data.refundFailureReason}</p>}
            {payment.data && (
              <p>
                Payment: <b>{label(payment.data.status || 'pending')}</b>
              </p>
            )}
            {expired && ['PENDING_PAYMENT', 'PAYMENT_FAILED'].includes(data.status) && (
              <p>The payment deadline has passed.</p>
            )}
            {payable && payment.data === null && (
              <button
                className="dh-button dh-button--primary"
                disabled={create.isPending}
                onClick={() => create.mutate()}
              >
                {create.isPending ? 'Preparing payment…' : 'Continue payment'}
              </button>
            )}
            {payment.data?.simulationEnabled && (
              <div className="notice">Local simulation — no real charge.</div>
            )}
            {canSimulate && (
              <div className="dh-actions">
                <button
                  className="dh-button dh-button--primary"
                  disabled={simulate.isPending}
                  onClick={() => simulate.mutate('SUCCEEDED')}
                >
                  Simulate successful payment
                </button>
                <button
                  className="dh-button"
                  disabled={simulate.isPending}
                  onClick={() => simulate.mutate('FAILED')}
                >
                  Simulate failed payment
                </button>
              </div>
            )}
            {payable && payment.data && !payment.data.simulationEnabled && (
              <p>Payment simulation is unavailable in this environment.</p>
            )}
            {data.status === 'FULFILLED' && (
              <div className="dh-actions">
                <a
                  href={
                    data.lines.some((line) => line.type === 'CREDIT_PRODUCT')
                      ? '/credits'
                      : '/rooms'
                  }
                  className="dh-button dh-button--primary"
                >
                  View your purchase
                </a>
                {!data.lines.some((line) => line.type === 'CREDIT_PRODUCT' && line.finalSale) && (
                  <button
                    className="dh-button"
                    disabled={refund.isPending}
                    onClick={() => refund.mutate()}
                  >
                    Request full refund
                  </button>
                )}
              </div>
            )}
            <button className="dh-button dh-button--ghost" onClick={refresh}>
              Refresh order
            </button>
          </article>
        </div>
      )}
    </>
  );
}

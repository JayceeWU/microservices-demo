'use client';
import type { FormEvent } from 'react';
import { useRouter } from 'next/navigation';
import { useMutation, useQuery } from '@tanstack/react-query';
import { api, type JsonValue, type Room } from '@dancehub/api-client';
import { ErrorBox, Page, usd } from '../workspace-shared';
import { checkoutKey, finishCheckout } from '../checkout-key';

export default function Rooms({
  studioId,
  notice,
}: {
  studioId: string;
  notice: (s: string) => void;
}) {
  const router = useRouter();
  const rooms = useQuery({
    queryKey: ['rooms', studioId],
    queryFn: () => api.rooms(studioId),
    enabled: !!studioId,
  });
  const hold = useMutation({
    mutationFn: async (value: Record<string, JsonValue>) => {
      const reservation = await api.createRoomHold({
        ...value,
        idempotencyKey: checkoutKey('room-hold', value),
      });
      return api.createRoomOrder(reservation.id, checkoutKey('room-order', reservation.id));
    },
    onSuccess: (order) => {
      router.push(`/orders?orderId=${encodeURIComponent(order.id)}`);
      finishCheckout('room-hold');
      finishCheckout('room-order');
      notice('Room held. Continue payment before the deadline shown on your order.');
    },
  });
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    hold.mutate({
      studioId,
      roomId: String(form.get('roomId')),
      startsAt: new Date(String(form.get('startsAt'))).toISOString(),
      endsAt: new Date(String(form.get('endsAt'))).toISOString(),
    });
  };
  return (
    <Page title="Reserve a studio room" subtitle="Room orders are paid separately from passes">
      <ErrorBox error={rooms.error || hold.error} />
      <form className="form" onSubmit={submit}>
        <label>
          Room
          <select className="dh-select" name="roomId" required>
            {rooms.data?.rooms.map((room: Room) => (
              <option value={room.id} key={room.id}>
                {room.name} · {usd.format((room.rentalRateCentsPerHour || 0) / 100)}/hr
              </option>
            ))}
          </select>
        </label>
        <label>
          Starts
          <input name="startsAt" type="datetime-local" required />
        </label>
        <label>
          Ends
          <input name="endsAt" type="datetime-local" required />
        </label>
        <button
          className="dh-button dh-button--primary"
          disabled={hold.isPending || !rooms.data?.rooms.length}
        >
          {hold.isPending ? 'Creating hold…' : 'Create hold & payment'}
        </button>
      </form>
    </Page>
  );
}

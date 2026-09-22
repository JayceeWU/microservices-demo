'use client';

import { useMutation, useQuery } from '@tanstack/react-query';
import { useRouter } from 'next/navigation';
import { api } from '@dancehub/api-client';
import { checkoutKey, finishCheckout } from './checkout-key';

export default function Cart({ notice }: { notice: (value: string) => void }) {
  const router = useRouter();
  const cart = useQuery({ queryKey: ['cart'], queryFn: api.cart });
  const checkout = useMutation({
    mutationFn: () => {
      const items = (cart.data?.items || []).map(({ productVersionId, quantity }) => ({
        productVersionId,
        quantity,
      }));
      return api.createOrder(items, checkoutKey('cart', items));
    },
    onSuccess: (order) => {
      router.push(`/orders?orderId=${encodeURIComponent(order.id)}`);
      finishCheckout('cart');
      notice('Order created. Review your order and continue payment.');
    },
  });
  return (
    <section className="page">
      <div className="section-title">
        <p>Student workspace</p>
        <h1>Cart & checkout</h1>
        <span>Items must share one issuer scope</span>
      </div>
      {(cart.error || checkout.error) && (
        <p className="error">{(cart.error || checkout.error)?.message}</p>
      )}
      <div className="list">
        {cart.data?.items.map((item) => (
          <article className="row" key={item.productVersionId}>
            <div>
              <b>Dance pass</b>
              <p>{item.productVersionId}</p>
            </div>
            <span>× {item.quantity}</span>
          </article>
        ))}
      </div>
      {cart.data && !cart.data.items.length && <p className="empty">No cart items yet.</p>}
      <button
        className="dh-button dh-button--primary"
        disabled={!cart.data?.items.length || checkout.isPending}
        onClick={() => checkout.mutate()}
      >
        {checkout.isPending ? 'Creating order…' : 'Continue to checkout'}
      </button>
    </section>
  );
}

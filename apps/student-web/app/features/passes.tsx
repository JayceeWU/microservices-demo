'use client';
import { useMutation, useQuery } from '@tanstack/react-query';
import { api, type CreditProduct } from '@dancehub/api-client';
import { ErrorBox, Page, usd } from '../workspace-shared';

export default function Passes({
  studioId,
  notice,
}: {
  studioId: string;
  notice: (s: string) => void;
}) {
  const q = useQuery({
    queryKey: ['products', studioId],
    queryFn: () => api.products(studioId),
    enabled: !!studioId,
  });
  const add = useMutation({
    mutationFn: (id: string) => api.addCartItem(id),
    onSuccess: () => notice('Added to cart'),
  });
  return (
    <Page title="Membership passes" subtitle="One checkout contains one issuer scope">
      <ErrorBox error={q.error || add.error} />
      <div className="grid">
        {q.data?.products.map((product: CreditProduct) => (
          <article className="card pass" key={product.id}>
            <p>
              {product.issuerScope === 'PLATFORM' ? 'Universal' : 'Studio pass'}{' '}
              {product.finalSale ? '· Final sale' : ''}
            </p>
            <h3>{product.name}</h3>
            <strong>{usd.format((product.amountCents || 0) / 100)}</strong>
            <small>
              {product.creditAmount ? `${product.creditAmount} credits` : 'Unlimited access'}
            </small>
            <button
              className="dh-button dh-button--primary"
              disabled={!product.id}
              onClick={() => product.id && add.mutate(product.id)}
            >
              Add to cart
            </button>
          </article>
        ))}
      </div>
    </Page>
  );
}

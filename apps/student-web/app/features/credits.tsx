'use client';
import { useQuery } from '@tanstack/react-query';
import { api, type Grant, type LedgerEntry } from '@dancehub/api-client';
import { ErrorBox, Page } from '../workspace-shared';

export default function Credits() {
  const balances = useQuery({ queryKey: ['balances'], queryFn: api.balances });
  const grants = useQuery({ queryKey: ['grants'], queryFn: api.grants });
  const ledger = useQuery({ queryKey: ['ledger'], queryFn: () => api.ledger({}) });
  return (
    <Page title="Credits & passes" subtitle="Balances, validity and immutable ledger">
      <ErrorBox error={balances.error || grants.error || ledger.error} />
      <div className="stats">
        {balances.data?.balances.map((balance) => (
          <article className="card" key={balance.studioId || 'platform'}>
            <span>{balance.studioId || 'Universal'}</span>
            <strong>{balance.unlimitedActive ? 'Unlimited' : balance.availableCredits}</strong>
          </article>
        ))}
      </div>
      <h2>Passes</h2>
      <div className="list">
        {grants.data?.grants.map((grant: Grant) => (
          <article className="row" key={grant.id}>
            <div>
              <b>{grant.unlimited ? 'Unlimited' : 'Credits'}</b>
              <p>
                {grant.status} · {grant.remainingCredits ?? 'Unlimited'} remaining
              </p>
            </div>
            <time>
              {grant.expiresAt ? new Date(grant.expiresAt).toLocaleDateString() : 'No expiry'}
            </time>
          </article>
        ))}
      </div>
      <h2>Ledger</h2>
      <div className="list">
        {ledger.data?.entries.map((entry: LedgerEntry) => (
          <article className="row" key={entry.id}>
            <b>{entry.kind}</b>
            <span>
              {(entry.creditDelta || 0) > 0 ? '+' : ''}
              {entry.creditDelta}
            </span>
            <p>{entry.reason}</p>
          </article>
        ))}
      </div>
    </Page>
  );
}

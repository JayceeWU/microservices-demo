import type { ReactNode } from 'react';

export const usd = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' });

export const Empty = ({ name }: { name: string }) => <div className="empty">No {name} yet.</div>;

export const ErrorBox = ({ error }: { error: unknown }) =>
  error ? (
    <div className="error" role="alert">
      {error instanceof Error ? error.message : 'Something went wrong'}
    </div>
  ) : null;

export function Page({
  title,
  subtitle,
  children,
}: {
  title: string;
  subtitle: string;
  children: ReactNode;
}) {
  return (
    <section className="page">
      <div className="section-title">
        <p>Student workspace</p>
        <h1>{title}</h1>
        <span>{subtitle}</span>
      </div>
      {children}
    </section>
  );
}

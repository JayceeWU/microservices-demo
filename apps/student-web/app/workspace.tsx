'use client';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api, getAuthContext, selectStudio } from '@dancehub/api-client';
import { useEffect, useState } from 'react';
import { signIn } from 'next-auth/react';
import dynamic from 'next/dynamic';
import { ErrorBox, Page } from './workspace-shared';
import { activeStudioOptions, defaultStudio } from './workspace-policy';
import { useWorkspaceSignOut } from './providers';

const nav = [
  ['home', 'Discover'],
  ['classes', 'Classes'],
  ['teachers', 'Teachers'],
  ['passes', 'Passes'],
  ['cart', 'Cart'],
  ['credits', 'Credits'],
  ['bookings', 'Bookings'],
  ['rooms', 'Rooms'],
  ['orders', 'Orders'],
  ['flash-sale', 'Flash sale'],
  ['chat', 'Chat'],
  ['profile', 'Profile'],
];
const Home = dynamic(() => import('./features/home'), {
  loading: () => <section className="loading">Loading page…</section>,
});
const Classes = dynamic(() => import('./features/classes'), {
  loading: () => <section className="loading">Loading page…</section>,
});
const Teachers = dynamic(() => import('./features/teachers'), {
  loading: () => <section className="loading">Loading page…</section>,
});
const Passes = dynamic(() => import('./features/passes'), {
  loading: () => <section className="loading">Loading page…</section>,
});
const Cart = dynamic(() => import('./cart'), {
  loading: () => <section className="loading">Loading page…</section>,
});
const Credits = dynamic(() => import('./features/credits'), {
  loading: () => <section className="loading">Loading page…</section>,
});
const Bookings = dynamic(() => import('./features/bookings'), {
  loading: () => <section className="loading">Loading page…</section>,
});
const Rooms = dynamic(() => import('./features/rooms'), {
  loading: () => <section className="loading">Loading page…</section>,
});
const Orders = dynamic(() => import('./features/orders'), {
  loading: () => <section className="loading">Loading page…</section>,
});
const FlashSale = dynamic(() => import('./features/flash-sale'), {
  loading: () => <section className="loading">Loading page…</section>,
});
const ChatPage = dynamic(() => import('./features/chat'), {
  loading: () => <section className="loading">Loading page…</section>,
});
const ProfilePage = dynamic(() => import('./features/profile'), {
  loading: () => <section className="loading">Loading page…</section>,
});

export default function Workspace({ section }: { section: string }) {
  const qc = useQueryClient();
  const [selectedStudioId, setSelectedStudioId] = useState('');
  const [notice, setNotice] = useState('');
  const logout = useWorkspaceSignOut();
  const memberships = useQuery({
    queryKey: ['memberships'],
    queryFn: api.memberships,
    retry: false,
  });
  const items = memberships.data?.memberships || [];
  const current = getAuthContext().studioId;
  const studioOptions = activeStudioOptions(items);
  const defaultStudioId = defaultStudio(studioOptions, current);
  const studioId = selectedStudioId || defaultStudioId;
  useEffect(() => {
    if (studioId) selectStudio(studioId);
  }, [studioId]);
  const chooseStudio = (id: string) => {
    setSelectedStudioId(id);
    selectStudio(id);
    qc.clear();
    setNotice('Studio changed');
  };
  if (memberships.isLoading) return <main className="loading">Loading your workspace…</main>;
  if (memberships.error)
    return (
      <main className="auth-card">
        <h1>Sign in to BayAreaDanceHub</h1>
        <p>Your roles and studios come from your authenticated account.</p>
        <ErrorBox error={memberships.error} />
        <button
          className="dh-button dh-button--primary"
          type="button"
          onClick={() => signIn('keycloak', { redirectTo: '/' })}
        >
          Sign in with Keycloak
        </button>
      </main>
    );
  return (
    <main>
      <header className="nav">
        <a className="brand" href="/">
          BayArea<span>Dance</span>Hub
        </a>
        <nav>
          {nav.map(([path, label]) => (
            <a
              className={section === path ? 'active' : ''}
              href={path === 'home' ? '/' : `/${path}`}
              key={path}
            >
              {label}
            </a>
          ))}
        </nav>
        <div className="workspace-controls dh-actions">
          <select
            className="dh-select"
            aria-label="Current studio"
            value={studioId}
            onChange={(e) => chooseStudio(e.target.value)}
          >
            <option value="">Select studio</option>
            {studioOptions.map((membership) => (
              <option key={membership.studioId} value={membership.studioId}>
                {membership.studioName}
              </option>
            ))}
          </select>
          <button
            className="dh-button dh-button--ghost"
            type="button"
            onClick={() => void logout()}
          >
            Sign out
          </button>
        </div>
      </header>
      {notice && <div className="notice">{notice}</div>}
      {section === 'home' && <Home />}
      {section === 'classes' && <Classes studioId={studioId} notice={setNotice} />}{' '}
      {section === 'teachers' && <Teachers studioId={studioId} notice={setNotice} />}{' '}
      {section === 'passes' && <Passes studioId={studioId} notice={setNotice} />}{' '}
      {section === 'cart' && <Cart notice={setNotice} />} {section === 'credits' && <Credits />}{' '}
      {section === 'bookings' && <Bookings notice={setNotice} />}{' '}
      {section === 'rooms' && <Rooms studioId={studioId} notice={setNotice} />}{' '}
      {section === 'orders' && <Orders />}{' '}
      {section === 'flash-sale' && <FlashSale notice={setNotice} />}{' '}
      {section === 'chat' && <ChatPage studioId={studioId} />}{' '}
      {section === 'profile' && <ProfilePage notice={setNotice} />}{' '}
      {!nav.some(([p]) => p === section) && (
        <Page title="Page not found" subtitle="">
          <a className="dh-button" href="/">
            Return home
          </a>
        </Page>
      )}
    </main>
  );
}

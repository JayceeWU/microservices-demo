'use client';
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import { SessionProvider, signIn, signOut, useSession } from 'next-auth/react';
import { createContext, useContext, useEffect, useRef, useState } from 'react';
import { api, clearAuthContext, initializeApiClient, setAuthContext } from '@dancehub/api-client';
import { initializeBrowserTelemetry } from '@dancehub/telemetry';

const LogoutContext = createContext<(() => Promise<void>) | null>(null);

export function useWorkspaceSignOut() {
  const logout = useContext(LogoutContext);
  if (!logout) throw new Error('Authenticated workspace is required');
  return logout;
}

function AccountIdentity({ children }: { children: React.ReactNode }) {
  const profile = useQuery({ queryKey: ['profile'], queryFn: api.profile, retry: false });
  const logout = useWorkspaceSignOut();
  if (profile.isPending) return <main className="auth-card">Loading your account…</main>;
  if (profile.error || !profile.data?.id) {
    return (
      <main className="auth-card">
        <h1>Unable to load your account</h1>
        <p role="alert">{profile.error?.message || 'Account is not provisioned.'}</p>
        <button
          className="dh-button dh-button--secondary"
          type="button"
          onClick={() => void profile.refetch()}
        >
          Retry
        </button>
        <button className="dh-button dh-button--ghost" type="button" onClick={() => void logout()}>
          Sign out
        </button>
      </main>
    );
  }
  // OIDC subject and the platform user UUID are distinct. Bind the authoritative
  // /me ID before checkout keys or other user-scoped browser state are created.
  setAuthContext({ userId: profile.data.id });
  return children;
}

function AuthenticatedQueries({
  children,
  accessToken,
}: {
  children: React.ReactNode;
  accessToken: string;
}) {
  // A different OIDC subject mounts a new boundary, isolating old in-flight work.
  const [client] = useState(() => {
    clearAuthContext();
    return new QueryClient({ defaultOptions: { queries: { staleTime: 30_000, retry: 2 } } });
  });
  const [signingOut, setSigningOut] = useState(false);
  const [logoutError, setLogoutError] = useState('');
  const mountedGeneration = useRef({ value: 0 });
  useEffect(() => {
    const lifecycle = mountedGeneration.current;
    const generation = ++lifecycle.value;
    return () => {
      // React Strict Mode immediately remounts effects in development. Only
      // dispose a client when that boundary really left the tree.
      queueMicrotask(() => {
        if (lifecycle.value === generation) {
          void client.cancelQueries();
          client.clear();
        }
      });
    };
  }, [client]);
  const logout = async () => {
    setSigningOut(true);
    setLogoutError('');
    clearAuthContext();
    try {
      await client.cancelQueries();
      client.clear();
      await signOut({ callbackUrl: '/' });
    } catch {
      setLogoutError('Unable to finish signing out. Please retry.');
    }
  };
  if (signingOut) {
    return (
      <main className="auth-card">
        <p role={logoutError ? 'alert' : 'status'}>{logoutError || 'Signing out…'}</p>
        {logoutError && (
          <button
            className="dh-button dh-button--secondary"
            type="button"
            onClick={() => void logout()}
          >
            Retry sign out
          </button>
        )}
      </main>
    );
  }
  // Set before descendants mount; token refresh preserves this user's cache.
  setAuthContext({ accessToken, userId: '' });
  return (
    <QueryClientProvider client={client}>
      <LogoutContext.Provider value={logout}>
        <AccountIdentity>{children}</AccountIdentity>
      </LogoutContext.Provider>
    </QueryClientProvider>
  );
}

function AuthBridge({ children }: { children: React.ReactNode }) {
  const { data, status } = useSession();
  if (status === 'loading') return <main className="auth-card">Loading secure session…</main>;
  if (status !== 'authenticated' || data?.authError || !data?.accessToken || !data?.subject) {
    clearAuthContext();
    return (
      <main className="auth-card">
        <h1>Sign in to BayAreaDanceHub</h1>
        <p>Your roles and studios come from your authenticated account.</p>
        <button
          className="dh-button dh-button--primary"
          type="button"
          onClick={() => void signIn('keycloak', { redirectTo: '/' })}
        >
          Sign in with Keycloak
        </button>
      </main>
    );
  }
  return (
    <AuthenticatedQueries key={data.subject} accessToken={data.accessToken}>
      {children}
    </AuthenticatedQueries>
  );
}
export function Providers({ children, apiUrl }: { children: React.ReactNode; apiUrl: string }) {
  initializeApiClient(apiUrl);
  useEffect(() => initializeBrowserTelemetry('student-web'), []);
  return (
    <SessionProvider>
      <AuthBridge>{children}</AuthBridge>
    </SessionProvider>
  );
}

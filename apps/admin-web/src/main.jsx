import React, { lazy, Suspense, useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter, NavLink, Route, Routes } from 'react-router-dom';
import {
  QueryClient,
  QueryClientProvider,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import { Button, Card, CardContent, LinearProgress, TextField } from '@mui/material';
import {
  api,
  getAuthContext,
  initializeApiClient,
  selectStudio,
  setAuthContext,
} from '@dancehub/api-client';
import { createOidcClient } from '@dancehub/auth';
import { initializeBrowserTelemetry } from '@dancehub/telemetry';
import '@dancehub/ui/tokens.css';
import '@dancehub/ui/controls.css';
import './styles.css';
import { Action, Empty, ErrorBox, Form, Page, useWorkspace, WorkspaceContext } from './shared.jsx';
import { resolveAdminWorkspace } from './workspace-policy.js';

const ChatInbox = lazy(() => import('./features/chat-inbox.jsx'));
const Platform = lazy(() => import('./features/platform.jsx'));
const Overview = lazy(() => import('./features/overview.jsx'));
const Schedule = lazy(() => import('./features/schedule.jsx'));
const Attendance = lazy(() => import('./features/attendance.jsx'));
const People = lazy(() => import('./features/people.jsx'));

initializeBrowserTelemetry('admin-web');
initializeApiClient(globalThis.DANCEHUB_API_URL);

const oidc = createOidcClient({
  issuer: globalThis.DANCEHUB_OIDC_ISSUER,
  clientId: 'admin-web',
  onToken: (accessToken) => setAuthContext({ accessToken, userId: '' }),
});

const theme = createTheme({
  palette: {
    primary: { main: '#7E22CE', dark: '#2E1065' },
    secondary: { main: '#DB2777' },
    background: { default: '#FAF7FC' },
  },
  shape: { borderRadius: 14 },
  typography: { fontFamily: 'Inter,Noto Sans SC,sans-serif' },
  components: {
    MuiButton: {
      defaultProps: { disableElevation: true, disableRipple: true },
      styleOverrides: {
        root: {
          minHeight: 'var(--control-height)',
          maxWidth: '100%',
          border: '1px solid transparent',
          borderRadius: 'var(--control-radius)',
          boxShadow: 'none',
          padding: '10px var(--control-padding-x)',
          fontFamily: 'var(--font-sans)',
          fontSize: 'var(--control-font-size)',
          fontWeight: 'var(--control-font-weight)',
          lineHeight: 1.5,
          textTransform: 'none',
          whiteSpace: 'nowrap',
          transition:
            'background-color var(--control-transition) ease, color var(--control-transition) ease, border-color var(--control-transition) ease, box-shadow var(--control-transition) ease',
          '&:focus-visible, &.Mui-focusVisible': {
            outline: '3px solid var(--control-focus)',
            outlineOffset: 3,
          },
          '&:active:not(.Mui-disabled)': { boxShadow: 'inset 0 2px 4px #2e106522' },
          '&.Mui-disabled': { opacity: 0.5, boxShadow: 'none' },
          '&.control-wrap': { whiteSpace: 'normal', textWrap: 'balance' },
          '@media (prefers-reduced-motion: reduce)': {
            transition: 'none',
          },
        },
        contained: ({ ownerState }) => ({
          borderColor: ownerState.color === 'error' ? '#fecdd3' : 'transparent',
          backgroundColor:
            ownerState.color === 'error' ? 'var(--control-danger-soft)' : 'var(--brand-pink)',
          color: ownerState.color === 'error' ? 'var(--danger)' : 'var(--surface)',
          '@media (hover: hover)': {
            '&:hover': {
              borderColor: ownerState.color === 'error' ? '#fda4af' : 'transparent',
              backgroundColor:
                ownerState.color === 'error' ? '#ffe4e6' : 'var(--control-primary-hover)',
            },
          },
          '&.Mui-disabled': {
            backgroundColor:
              ownerState.color === 'error' ? 'var(--control-danger-soft)' : 'var(--brand-pink)',
            color: ownerState.color === 'error' ? 'var(--danger)' : 'var(--surface)',
          },
        }),
        outlined: ({ ownerState }) => ({
          borderColor: ownerState.color === 'error' ? '#fecdd3' : 'var(--border)',
          backgroundColor:
            ownerState.color === 'error'
              ? 'var(--control-danger-soft)'
              : 'var(--brand-purple-soft)',
          color: ownerState.color === 'error' ? 'var(--danger)' : 'var(--brand-purple)',
          '@media (hover: hover)': {
            '&:hover': {
              borderColor: ownerState.color === 'error' ? '#fda4af' : '#d8b4fe',
              backgroundColor:
                ownerState.color === 'error' ? '#ffe4e6' : 'var(--control-secondary-hover)',
            },
          },
          '&.Mui-disabled': {
            borderColor: ownerState.color === 'error' ? '#fecdd3' : 'var(--border)',
            color: ownerState.color === 'error' ? 'var(--danger)' : 'var(--brand-purple)',
          },
        }),
        text: ({ ownerState }) => ({
          borderColor: ownerState.color === 'inherit' ? '#ffffff40' : 'transparent',
          color:
            ownerState.color === 'inherit'
              ? '#f3e8ff'
              : ownerState.color === 'error'
                ? 'var(--danger)'
                : 'var(--brand-purple)',
          '@media (hover: hover)': {
            '&:hover': {
              borderColor: ownerState.color === 'inherit' ? '#ffffff80' : 'transparent',
              backgroundColor:
                ownerState.color === 'inherit'
                  ? '#ffffff18'
                  : ownerState.color === 'error'
                    ? 'var(--control-danger-soft)'
                    : 'var(--brand-purple-soft)',
            },
          },
          '&:focus-visible, &.Mui-focusVisible': {
            outlineColor: ownerState.color === 'inherit' ? '#e9d5ff' : 'var(--control-focus)',
          },
        }),
      },
    },
  },
});
const client = new QueryClient();
function WorkspaceProvider({ children }) {
  const qc = useQueryClient();
  const memberships = useQuery({
    queryKey: ['memberships'],
    queryFn: api.memberships,
    retry: false,
  });
  const profile = useQuery({ queryKey: ['profile'], queryFn: api.profile, retry: false });
  const globalPlatformAdmin = (memberships.data?.globalRoles || []).includes('platform_admin');
  const platformStudios = useQuery({
    queryKey: ['platform-studio-options'],
    queryFn: () => api.studios(),
    enabled: globalPlatformAdmin,
  });
  const [selectedStudioId, setSelectedStudioId] = useState('');
  const current = getAuthContext().studioId;
  const { allowed, platformAdmin, studioOptions, defaultStudioId } = resolveAdminWorkspace(
    memberships.data,
    platformStudios.data?.studios,
    current,
  );
  const studioId = selectedStudioId || defaultStudioId;
  useEffect(() => {
    if (studioId) selectStudio(studioId);
  }, [studioId]);
  const changeStudio = (id) => {
    setSelectedStudioId(id);
    selectStudio(id);
    qc.clear();
  };
  if (memberships.isLoading || profile.isLoading)
    return <main className="gate">Loading administrator workspace…</main>;
  if (memberships.error || (!allowed.length && !platformAdmin))
    return (
      <main className="gate">
        <h1>Administrator access required</h1>
        <ErrorBox error={memberships.error} />
        <p>This workspace derives authorization from your memberships.</p>
        <a className="dh-button dh-button--primary" href="/login">
          Sign in with Keycloak
        </a>
      </main>
    );
  return (
    <WorkspaceContext.Provider
      value={{
        allowed,
        studioOptions,
        studioId,
        changeStudio,
        profile: profile.data,
        platformAdmin,
      }}
    >
      {children}
    </WorkspaceContext.Provider>
  );
}

const nav = [
  ['/', 'Overview'],
  ['/schedule', 'Schedule approvals'],
  ['/attendance', 'Classes & attendance'],
  ['/people', 'Teachers'],
  ['/catalog', 'Rooms & passes'],
  ['/refunds', 'Refunds'],
  ['/credits', 'Transfer & pause'],
  ['/payroll', 'Payroll'],
  ['/analytics', 'Analytics'],
  ['/chat', 'Studio inbox & groups'],
];
function Shell() {
  const w = useWorkspace();
  return (
    <div className="shell">
      <aside>
        <div className="logo">
          BA<span>DH</span>
        </div>
        <p>Admin workspace</p>
        <nav>
          {nav.map(([to, label]) => (
            <NavLink end={to === '/'} key={to} to={to}>
              {label}
            </NavLink>
          ))}
          {w.platformAdmin && <NavLink to="/platform">Platform admin</NavLink>}
        </nav>
        <Button
          className="admin-sign-out"
          variant="text"
          color="inherit"
          onClick={() => oidc.logout()}
        >
          Sign out
        </Button>
      </aside>
      <main>
        <header>
          <div>
            <p className="eyebrow">Current studio</p>
            <h1>{w.profile?.displayName || 'Administrator'}</h1>
          </div>
          <select
            className="dh-select studio-select"
            aria-label="Current studio"
            value={w.studioId}
            onChange={(e) => w.changeStudio(e.target.value)}
          >
            <option value="">Select studio</option>
            {w.studioOptions
              .filter((m) => m.studioId)
              .map((m) => (
                <option value={m.studioId} key={m.studioId}>
                  {m.studioName || m.studioId}
                </option>
              ))}
          </select>
        </header>
        <Suspense fallback={<LinearProgress aria-label="Loading page" />}>
          <Routes>
            <Route path="/" element={<Overview />} />
            <Route path="/schedule" element={<Schedule />} />
            <Route path="/attendance" element={<Attendance />} />
            <Route path="/people" element={<People />} />
            <Route path="/catalog" element={<Catalog />} />
            <Route path="/refunds" element={<Refunds />} />
            <Route path="/credits" element={<CreditOps />} />
            <Route path="/payroll" element={<Payroll />} />
            <Route path="/analytics" element={<Analytics />} />
            <Route path="/chat" element={<ChatInbox />} />
            <Route path="/platform" element={<Platform />} />
            <Route path="*" element={<Empty>Page not found.</Empty>} />
          </Routes>
        </Suspense>
      </main>
    </div>
  );
}

function Catalog() {
  const w = useWorkspace();
  const rooms = useQuery({
    queryKey: ['rooms', w.studioId],
    queryFn: () => api.rooms(w.studioId),
    enabled: !!w.studioId,
  });
  const products = useQuery({
    queryKey: ['products', w.studioId],
    queryFn: () => api.products(w.studioId),
    enabled: !!w.studioId,
  });
  return (
    <Page title="Rooms & membership products">
      <ErrorBox error={rooms.error || products.error} />
      <h3>Rooms</h3>
      <div className="cards">
        {rooms.data?.rooms?.map((r) => (
          <Card key={r.id}>
            <CardContent>
              <b>{r.name}</b>
              <p>
                {r.capacity} people · ${(r.rentalRateCentsPerHour / 100).toFixed(2)}/hr
              </p>
            </CardContent>
          </Card>
        ))}
      </div>
      <h3>Published passes</h3>
      <div className="cards">
        {products.data?.products?.map((p) => (
          <Card key={p.id}>
            <CardContent>
              <b>{p.name}</b>
              <p>
                ${(p.amountCents / 100).toFixed(2)} ·{' '}
                {p.finalSale ? 'Final sale' : 'Refund eligible if unused'}
              </p>
            </CardContent>
          </Card>
        ))}
      </div>
    </Page>
  );
}
function Refunds() {
  const approve = useMutation({ mutationFn: (v) => api.approveRefund(v.orderId, v.reason) });
  const order = useQuery({
    queryKey: ['refund-order', approve.data?.id],
    queryFn: () => api.order(approve.data.id),
    enabled: !!approve.data?.id,
    refetchInterval: (query) => (query.state.data?.status === 'REFUND_PENDING' ? 2000 : false),
  });
  return (
    <Page title="Refund review">
      <Form onSubmit={(v) => approve.mutate(v)}>
        <TextField name="orderId" label="Order ID" required />
        <TextField name="reason" label="Decision reason" required />
        <Button
          className="control-wrap"
          type="submit"
          variant="contained"
          disabled={approve.isPending}
        >
          Approve full original-method refund
        </Button>
      </Form>
      <ErrorBox error={approve.error || order.error} />
      {approve.isSuccess && (
        <div className="notice" role="status">
          <p>Order: {order.data?.status || approve.data.status}</p>
          <p>Refund stage: {order.data?.refundPhase || approve.data.refundPhase || 'Processing'}</p>
          {order.data?.refundFailureReason && <p>{order.data.refundFailureReason}</p>}
        </div>
      )}
    </Page>
  );
}
function CreditOps() {
  const [message, setMessage] = useState('');
  const transfer = useMutation({
    mutationFn: (v) => api.transferGrant(v.grantId, v.targetUserId, v.reason),
  });
  const pause = useMutation({ mutationFn: (v) => api.pauseGrant(v.grantId, v.reason) });
  const resume = useMutation({ mutationFn: (v) => api.resumeGrant(v.grantId, v.reason) });
  const run = (m, v) =>
    m.mutate(v, { onSuccess: () => setMessage('Credit operation completed and audited') });
  return (
    <Page title="Transfer, pause & resume cards">
      <p>
        Used and promotional cards may be transferred by an authorized studio administrator. Only
        the remaining entitlement moves.
      </p>
      {message && <p className="notice">{message}</p>}
      <div className="form-grid">
        <Action
          title="Transfer card"
          fields={['grantId', 'targetUserId', 'reason']}
          submit={(v) => run(transfer, v)}
        />
        <Action title="Pause card" fields={['grantId', 'reason']} submit={(v) => run(pause, v)} />
        <Action title="Resume card" fields={['grantId', 'reason']} submit={(v) => run(resume, v)} />
      </div>
      <ErrorBox error={transfer.error || pause.error || resume.error} />
    </Page>
  );
}
function Payroll() {
  const w = useWorkspace();
  const [month, setMonth] = useState(new Date().toISOString().slice(0, 7));
  const q = useQuery({
    queryKey: ['payroll', w.studioId, month],
    queryFn: () => api.monthlyPayroll(w.studioId, month),
    enabled: !!w.studioId && /^\d{4}-(0[1-9]|1[0-2])$/.test(month),
    refetchInterval: 5000,
  });
  return (
    <Page title="Monthly payroll">
      <input
        type="month"
        aria-label="Payroll month"
        value={month}
        onChange={(e) => setMonth(e.target.value)}
      />
      <ErrorBox error={q.error} />
      <h2>${((q.data?.totalAmountCents || 0) / 100).toFixed(2)} USD</h2>
      {q.data?.lines?.map((l) => (
        <article className="row" key={l.classSessionId} data-session-id={l.classSessionId}>
          <div>
            <b>{l.teacherId}</b>
            <p>
              {l.approvedDurationMinutes} min · {l.redemptionCount} students
            </p>
          </div>
          <strong>${(l.totalAmountCents / 100).toFixed(2)}</strong>
        </article>
      ))}
    </Page>
  );
}
function Analytics() {
  const w = useWorkspace();
  const q = useQuery({
    queryKey: ['analytics', w.studioId],
    queryFn: () => api.analytics(w.studioId),
    enabled: !!w.studioId,
  });
  return (
    <Page title="Studio analytics">
      <ErrorBox error={q.error} />
      {q.isLoading && <LinearProgress />}
      {q.data ? (
        <pre className="result">{JSON.stringify(q.data, null, 2)}</pre>
      ) : (
        <Empty>No analytics projection is available yet.</Empty>
      )}
    </Page>
  );
}
async function start() {
  if (globalThis.DANCEHUB_AUTH_MODE !== 'dev') {
    const session = await oidc.initialize();
    if (!session) {
      await oidc.login();
      return;
    }
  }
  createRoot(document.getElementById('root')).render(
    <React.StrictMode>
      <ThemeProvider theme={theme}>
        <QueryClientProvider client={client}>
          <BrowserRouter>
            <WorkspaceProvider>
              <Shell />
            </WorkspaceProvider>
          </BrowserRouter>
        </QueryClientProvider>
      </ThemeProvider>
    </React.StrictMode>,
  );
}
start().catch((error) => {
  console.error(error);
  document.getElementById('root').innerHTML =
    '<main class="gate"><h1>Unable to complete sign in</h1><p>Please reload and try again.</p></main>';
});

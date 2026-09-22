import { Card, CardContent, Chip, LinearProgress } from '@mui/material';
import { useQuery } from '@tanstack/react-query';
import { api } from '@dancehub/api-client';
import { ErrorBox, MoreSessions, useSessions, useWorkspace } from '../shared.jsx';

export default function Overview() {
  const w = useWorkspace();
  const sessions = useSessions();
  const month = new Date().toISOString().slice(0, 7);
  const payroll = useQuery({
    queryKey: ['payroll', w.studioId, month],
    queryFn: () => api.monthlyPayroll(w.studioId, month),
    enabled: !!w.studioId,
    refetchInterval: 5000,
  });
  return (
    <>
      <div className="welcome">
        <p>Operational overview</p>
        <h2>Today at your studio</h2>
      </div>
      <section className="metrics">
        <Metric label="Classes loaded" value={sessions.data?.sessions?.length || 0} />
        <Metric
          label="Approvals in loaded classes"
          value={
            sessions.data?.sessions?.filter((s) => s.status === 'PENDING_APPROVAL').length || 0
          }
        />
        <Metric
          label="Monthly payroll"
          value={new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format(
            (payroll.data?.totalAmountCents || 0) / 100,
          )}
        />
      </section>
      <Card>
        <CardContent>
          <h2>Class operations</h2>
          {sessions.isLoading && <LinearProgress />}
          <ErrorBox error={sessions.error || payroll.error} />
          {sessions.data?.sessions?.map((s) => (
            <article className="row" key={s.id}>
              <div>
                <h3>{s.title}</h3>
                <p>
                  {new Date(s.startsAt).toLocaleString()} · {s.confirmedCount}/{s.capacity}
                </p>
              </div>
              <Chip label={s.status.replaceAll('_', ' ')} color="secondary" variant="outlined" />
            </article>
          ))}
          <MoreSessions query={sessions} />
        </CardContent>
      </Card>
    </>
  );
}
function Metric({ label, value }) {
  return (
    <Card>
      <CardContent>
        <span>{label}</span>
        <strong>{value}</strong>
        <small>Current studio</small>
      </CardContent>
    </Card>
  );
}

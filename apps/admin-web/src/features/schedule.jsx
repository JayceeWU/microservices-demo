import { Button, Card, CardContent, TextField } from '@mui/material';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '@dancehub/api-client';
import { Empty, ErrorBox, Form, MoreSessions, Page, useSessions } from '../shared.jsx';

export default function Schedule() {
  const q = useSessions(),
    qc = useQueryClient();
  const approve = useMutation({
    mutationFn: ({ id, minimumStudents }) =>
      api.approveSchedule(id, {
        approve: true,
        minimumStudents: Number(minimumStudents),
        reason: 'Admin approval',
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin-sessions'] }),
  });
  return (
    <Page title="Schedule approvals">
      <ErrorBox error={q.error || approve.error} />
      {q.data?.sessions
        ?.filter((s) => s.status === 'PENDING_APPROVAL')
        .map((s) => (
          <Card key={s.id}>
            <CardContent>
              <h3>{s.title}</h3>
              <p>
                {new Date(s.startsAt).toLocaleString()} · capacity {s.capacity}
              </p>
              <Form onSubmit={(v) => approve.mutate({ id: s.id, ...v })}>
                <TextField
                  name="minimumStudents"
                  label="Minimum students"
                  type="number"
                  defaultValue={4}
                />
                <Button type="submit" variant="contained">
                  Approve & freeze
                </Button>
              </Form>
            </CardContent>
          </Card>
        ))}
      {q.data && !q.data.sessions?.some((s) => s.status === 'PENDING_APPROVAL') && (
        <Empty>
          {q.hasNextPage
            ? 'No pending requests in the loaded classes. Load more to continue.'
            : 'No pending schedule requests.'}
        </Empty>
      )}
      <MoreSessions query={q} />
    </Page>
  );
}

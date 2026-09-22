import { Button, TextField } from '@mui/material';
import { useMutation } from '@tanstack/react-query';
import { api } from '@dancehub/api-client';
import { ErrorBox, Form, Page, useWorkspace } from '../shared.jsx';

export default function People() {
  const w = useWorkspace();
  const invite = useMutation({ mutationFn: (v) => api.inviteTeacher(w.studioId, v.email) });
  return (
    <Page title="Teachers & invitations">
      <Form onSubmit={(v) => invite.mutate(v)}>
        <TextField name="email" type="email" label="Teacher email" required />
        <Button type="submit" variant="contained">
          Send invitation
        </Button>
      </Form>
      <ErrorBox error={invite.error} />
      {invite.isSuccess && <p className="notice">Invitation created.</p>}
    </Page>
  );
}

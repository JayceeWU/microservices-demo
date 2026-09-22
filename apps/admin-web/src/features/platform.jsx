import { useState } from 'react';
import { Button, Card, CardContent, Chip, LinearProgress, TextField } from '@mui/material';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@dancehub/api-client';
import { ErrorBox, Form, Page, useWorkspace } from '../shared.jsx';

export default function Platform() {
  const w = useWorkspace();
  const qc = useQueryClient();
  const studios = useQuery({ queryKey: ['platform-studios'], queryFn: () => api.studios() });
  const assignments = useQuery({
    queryKey: ['global-role-assignments'],
    queryFn: api.globalRoleAssignments,
    enabled: w.platformAdmin,
  });
  const [lookup, setLookup] = useState(null);
  const [lookupError, setLookupError] = useState(null);
  const [reason, setReason] = useState('');
  const lookupUser = async ({ email }) => {
    setLookupError(null);
    try {
      setLookup(await api.lookupPlatformUser(String(email).trim()));
    } catch (error) {
      setLookup(null);
      setLookupError(error);
    }
  };
  const grant = useMutation({
    mutationFn: () => api.grantGlobalRole(lookup.user.id, reason.trim()),
    onSuccess: async () => {
      setLookup(null);
      setReason('');
      await qc.invalidateQueries({ queryKey: ['global-role-assignments'] });
    },
  });
  const revoke = useMutation({
    mutationFn: ({ id }) => api.revokeGlobalRole(id, reason.trim()),
    onSuccess: async () => {
      setReason('');
      await qc.invalidateQueries({ queryKey: ['global-role-assignments'] });
    },
  });
  if (!w.platformAdmin)
    return (
      <Page title="Forbidden">
        <p className="error">Platform administrator role is required.</p>
      </Page>
    );
  return (
    <Page title="Platform administration">
      <p>Cross-tenant view. Every mutation still records platform-admin identity.</p>
      <ErrorBox error={assignments.error || grant.error || revoke.error} />
      <Form onSubmit={lookupUser}>
        <h3>Find an existing account</h3>
        <TextField name="email" label="Exact email address" type="email" required />
        <Button type="submit" variant="outlined">
          Look up user
        </Button>
        <ErrorBox error={lookupError} />
        {lookup && (
          <div className="lookup-result">
            <strong>{lookup.user.displayName}</strong>
            <span>{lookup.user.email}</span>
            <Chip
              label={
                (lookup.globalRoles || []).includes('platform_admin')
                  ? 'Already a platform admin'
                  : 'No global role'
              }
            />
          </div>
        )}
        <TextField
          value={reason}
          onChange={(event) => setReason(event.target.value)}
          label="Audit reason"
        />
        <Button
          type="button"
          variant="contained"
          className="control-wrap"
          disabled={
            !lookup ||
            !reason.trim() ||
            (lookup.globalRoles || []).includes('platform_admin') ||
            grant.isPending
          }
          onClick={() => grant.mutate()}
        >
          Grant platform administrator
        </Button>
      </Form>
      <h3>Current platform administrators</h3>
      {assignments.isLoading && <LinearProgress />}
      {assignments.data?.assignments?.map((assignment) => {
        const self = assignment.user.id === w.profile?.id;
        return (
          <Card key={assignment.id}>
            <CardContent className="role-assignment">
              <div>
                <strong>{assignment.user.displayName}</strong>
                <p>{assignment.user.email}</p>
              </div>
              <Button
                color="error"
                variant="outlined"
                disabled={self || !reason.trim() || revoke.isPending}
                onClick={() => revoke.mutate({ id: assignment.id })}
              >
                {self ? 'Cannot revoke yourself' : 'Revoke'}
              </Button>
            </CardContent>
          </Card>
        );
      })}
      <div className="cards">
        {studios.data?.studios?.map((s) => (
          <Card key={s.id}>
            <CardContent>
              <h3>{s.name}</h3>
              <p>
                {s.city}, {s.state}
              </p>
            </CardContent>
          </Card>
        ))}
      </div>
    </Page>
  );
}

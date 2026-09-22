import { useState } from 'react';
import { Button, TextField } from '@mui/material';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '@dancehub/api-client';
import { Action, ErrorBox, Form, Page } from '../shared.jsx';
import { createAttendanceCommands } from '../attendance-commands.js';

export default function Attendance() {
  const [message, setMessage] = useState('');
  const [command] = useState(() => createAttendanceCommands(api));
  const qc = useQueryClient();
  const options = (operation) => ({
    mutationFn: async (value) => {
      try {
        return await command(operation, value);
      } catch (error) {
        if (error.status === 503)
          setMessage('Request received; processing credits. Retrying automatically.');
        throw error;
      }
    },
    retry: (count, error) => count < 3 && error.status === 503,
    retryDelay: 2000,
    onError: (error) => {
      if (error.status === 503)
        setMessage('Request is still processing. Submit the same form to retry safely.');
    },
  });
  const complete = useMutation({ mutationFn: (v) => api.completeClass(v.sessionId, v.reason) });
  const cancel = useMutation({ mutationFn: (v) => api.cancelClass(v.sessionId, v.reason) });
  const walk = useMutation(options('walk'));
  const correct = useMutation(options('correct'));
  const reverse = useMutation(options('reverse'));
  const pending =
    complete.isPending ||
    cancel.isPending ||
    walk.isPending ||
    correct.isPending ||
    reverse.isPending;
  const run = (mutation, v) => {
    setMessage('');
    mutation.mutate(v, {
      onSuccess: () => {
        setMessage('Operation completed');
        qc.invalidateQueries({ queryKey: ['admin-sessions'] });
        qc.invalidateQueries({ queryKey: ['payroll'] });
      },
    });
  };
  return (
    <Page title="Attendance & completion">
      <p>
        All corrections require an audit reason and remain open only in their server-enforced
        windows.
      </p>
      {message && <p className="notice">{message}</p>}
      <div className="form-grid">
        <Action
          title="Confirm class completed"
          pending={pending}
          fields={['sessionId', 'reason']}
          submit={(v) => run(complete, v)}
        />
        <Action
          title="Cancel class by studio"
          pending={pending}
          destructive
          fields={['sessionId', 'reason']}
          submit={(v) => run(cancel, v)}
        />
        <Action
          title="Add walk-in redemption"
          pending={pending}
          fields={['sessionId', 'studentId', 'reason']}
          submit={(v) => run(walk, v)}
        />
        <Form onSubmit={(v) => run(correct, v)}>
          <h3>Correct attendance</h3>
          <TextField name="bookingId" label="Booking ID" required />
          <select className="dh-select" name="attended" aria-label="Attendance status">
            <option value="true">Attended</option>
            <option value="false">No show</option>
          </select>
          <TextField name="reason" label="Audit reason" required />
          <Button type="submit" variant="contained" disabled={pending}>
            Apply correction
          </Button>
        </Form>
        <Action
          title="Reverse redemption"
          destructive
          pending={pending}
          fields={['bookingId', 'reason']}
          submit={(v) => run(reverse, v)}
        />
      </div>
      <ErrorBox
        error={complete.error || cancel.error || walk.error || correct.error || reverse.error}
      />
    </Page>
  );
}

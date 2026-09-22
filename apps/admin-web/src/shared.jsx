import { createContext, useContext } from 'react';
import { useInfiniteQuery } from '@tanstack/react-query';
import { api } from '@dancehub/api-client';
import { Button, TextField } from '@mui/material';

export const WorkspaceContext = createContext(null);

export const useWorkspace = () => useContext(WorkspaceContext);

export function useSessions() {
  const { studioId } = useWorkspace();
  const query = useInfiniteQuery({
    queryKey: ['admin-sessions', studioId],
    initialPageParam: '',
    queryFn: ({ pageParam }) => api.sessions({ studio_id: studioId, page_token: pageParam }),
    getNextPageParam: (page) => page.nextPageToken || undefined,
    enabled: Boolean(studioId),
  });
  return {
    ...query,
    data: query.data ? { sessions: query.data.pages.flatMap((page) => page.sessions) } : undefined,
  };
}

export function MoreSessions({ query }) {
  return query.hasNextPage ? (
    <Button
      disabled={query.isFetching}
      onClick={() => query.fetchNextPage({ cancelRefetch: false })}
    >
      Load more classes
    </Button>
  ) : null;
}

export const ErrorBox = ({ error }) =>
  error ? (
    <p className="error" role="alert">
      {error.message || String(error)}
    </p>
  ) : null;

export const Empty = ({ children }) => <p className="empty">{children}</p>;

export const Form = ({ children, onSubmit }) => (
  <form
    className="admin-form"
    onSubmit={(event) => {
      event.preventDefault();
      onSubmit(Object.fromEntries(new FormData(event.currentTarget)));
    }}
  >
    {children}
  </form>
);

export function Action({ title, fields, submit, destructive = false, pending = false }) {
  return (
    <Form onSubmit={submit}>
      <h3>{title}</h3>
      {fields.map((field) => (
        <TextField
          key={field}
          name={field}
          label={field === 'reason' ? 'Audit reason' : field}
          required
        />
      ))}
      <Button
        type="submit"
        variant="contained"
        disabled={pending}
        color={destructive ? 'error' : 'primary'}
      >
        Submit
      </Button>
    </Form>
  );
}

export function Page({ title, children }) {
  return (
    <section className="admin-page">
      <p className="eyebrow">BayAreaDanceHub operations</p>
      <h2>{title}</h2>
      {children}
    </section>
  );
}

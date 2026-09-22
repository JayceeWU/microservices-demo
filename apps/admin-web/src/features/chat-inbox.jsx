import { useEffect, useState } from 'react';
import { Button, Card, CardContent, TextField } from '@mui/material';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, chatFrameScope, ChatSocket } from '@dancehub/api-client';
import { ErrorBox, Form, Page, useWorkspace } from '../shared.jsx';

export default function ChatInbox() {
  const workspace = useWorkspace();
  const queryClient = useQueryClient();
  const [selected, setSelected] = useState('');
  const [body, setBody] = useState('');
  const [connected, setConnected] = useState(false);
  const [attachment, setAttachment] = useState(null);
  const [uploadProgress, setUploadProgress] = useState(0);
  const [uploadStatus, setUploadStatus] = useState('idle');
  const [uploadError, setUploadError] = useState(null);
  const [sendError, setSendError] = useState(null);
  const [socket] = useState(() => new ChatSocket());
  const conversations = useQuery({ queryKey: ['admin-chat'], queryFn: api.chatConversations });
  const groups = useQuery({
    queryKey: ['admin-chat-groups', workspace.studioId],
    queryFn: () => api.chatGroups({ studio_id: workspace.studioId }),
    enabled: Boolean(workspace.studioId),
  });
  const messages = useQuery({
    queryKey: ['admin-chat-messages', selected],
    queryFn: () => api.chatMessages(selected),
    enabled: Boolean(selected),
  });
  useEffect(() => {
    const last = (messages.data?.messages || []).reduce(
      (highest, message) => Math.max(highest, Number(message.sequence || 0)),
      0,
    );
    // The socket drops receipts that do not advance, so this cannot loop with the refresh.
    if (connected && selected) socket.markRead(selected, last);
  }, [connected, messages.data?.messages, selected, socket]);
  const createGroup = useMutation({
    mutationFn: (value) => api.createChatGroup({ kind: 3, studioId: workspace.studioId, ...value }),
    onSuccess: (value) => {
      setSelected(value.id || '');
      queryClient.invalidateQueries({ queryKey: ['admin-chat'] });
      queryClient.invalidateQueries({ queryKey: ['admin-chat-groups'] });
    },
  });
  const ban = useMutation({
    mutationFn: (value) => api.banChatMember(value.conversationId, value.userId, value.reason),
  });
  const unban = useMutation({
    mutationFn: (value) => api.unbanChatMember(value.conversationId, value.userId, value.reason),
  });
  const deleteGroup = useMutation({
    mutationFn: ({ conversationId, reason }) => api.deleteChatGroup(conversationId, reason),
    onSuccess: () => {
      setSelected('');
      queryClient.invalidateQueries({ queryKey: ['admin-chat'] });
      queryClient.invalidateQueries({ queryKey: ['admin-chat-groups'] });
    },
  });
  const selectedGroup = groups.data?.groups?.find((group) => group.id === selected);
  useEffect(() => {
    const refresh = () => {
      setConnected(true);
      queryClient.invalidateQueries({ queryKey: ['admin-chat'] });
      if (selected) queryClient.invalidateQueries({ queryKey: ['admin-chat-messages', selected] });
    };
    const onFrame = (event) => {
      const scope = chatFrameScope(event.detail);
      if (scope === 'thread') refresh();
      else if (scope === 'conversations') {
        queryClient.invalidateQueries({ queryKey: ['admin-chat'] });
      }
    };
    const disconnected = () => setConnected(false);
    socket.addEventListener('connected', refresh);
    socket.addEventListener('frame', onFrame);
    socket.addEventListener('disconnected', disconnected);
    socket.connect().catch(() => setConnected(false));
    return () => {
      socket.removeEventListener('connected', refresh);
      socket.removeEventListener('frame', onFrame);
      socket.removeEventListener('disconnected', disconnected);
      socket.close();
    };
  }, [queryClient, selected, socket]);
  const send = (event) => {
    event.preventDefault();
    if (selected && (body.trim() || attachment)) {
      try {
        socket.sendMessage(
          selected,
          body.trim(),
          attachment ? [attachment.id] : [],
          attachment?.kind,
        );
      } catch (error) {
        // Keep the draft: the socket is reconnecting and the message was not delivered.
        setSendError(error);
        return;
      }
      setSendError(null);
      setBody('');
      setAttachment(null);
      setUploadProgress(0);
      setUploadStatus('idle');
    }
  };
  return (
    <Page title="Studio inbox & groups">
      <p className={connected ? 'notice' : 'error'}>
        {connected ? 'Realtime connected' : 'Reconnecting…'}
      </p>
      <div className="cards">
        <Form onSubmit={(value) => createGroup.mutate(value)}>
          <TextField name="title" label="New studio group" required />
          <TextField name="description" label="Description" />
          <Button type="submit" variant="contained">
            Create group
          </Button>
        </Form>
        <Form onSubmit={(value) => ban.mutate({ ...value, conversationId: selected })}>
          <TextField name="userId" label="Member ID" required />
          <TextField name="reason" label="Audit reason" required />
          <Button type="submit" variant="outlined" color="error" disabled={!selected}>
            Ban
          </Button>
          <Button
            type="button"
            variant="outlined"
            disabled={!selected}
            onClick={(event) => {
              const form = event.currentTarget.form;
              const values = Object.fromEntries(new FormData(form));
              unban.mutate({ ...values, conversationId: selected });
            }}
          >
            Unban
          </Button>
        </Form>
      </div>
      <ErrorBox
        error={
          conversations.error ||
          groups.error ||
          messages.error ||
          createGroup.error ||
          ban.error ||
          unban.error ||
          deleteGroup.error ||
          uploadError ||
          sendError
        }
      />
      <div className="chat-layout">
        <Card>
          <CardContent>
            <h3>Shared inbox</h3>
            {conversations.data?.conversations
              ?.filter((conversation) => conversation.kind === 'CONVERSATION_KIND_STUDIO_SUPPORT')
              .map((conversation) => (
                <button
                  type="button"
                  className="dh-choice"
                  aria-pressed={selected === conversation.id}
                  key={conversation.id}
                  onClick={() => setSelected(conversation.id || '')}
                >
                  {conversation.title || conversation.kind} (
                  {Math.max(
                    0,
                    Number(conversation.lastSequence || 0) -
                      Number(conversation.lastReadSequence || 0),
                  )}
                  )
                </button>
              ))}
            <h3>Studio & teacher groups</h3>
            {groups.data?.groups?.map((group) => (
              <button
                type="button"
                className="dh-choice"
                aria-pressed={selected === group.id}
                key={group.id}
                onClick={() => setSelected(group.id || '')}
              >
                {group.title || group.kind} · {group.memberCount || 0}
              </button>
            ))}
            {selectedGroup && (
              <Button
                color="error"
                variant="outlined"
                className="chat-delete-group"
                onClick={() => {
                  const reason = window.prompt('Audit reason for deleting this group');
                  if (reason?.trim())
                    deleteGroup.mutate({ conversationId: selectedGroup.id, reason: reason.trim() });
                }}
              >
                Delete group
              </Button>
            )}
          </CardContent>
        </Card>
        <Card>
          <CardContent className="chat-thread">
            {(messages.data?.messages || [])
              .slice()
              .reverse()
              .map((message) => (
                <article key={message.id}>
                  <small>{message.senderDisplayName || message.senderId}</small>
                  <p>{message.withdrawnAt ? 'Message withdrawn' : message.body}</p>
                  <div className="dh-actions">
                    {message.attachments?.map((item) => (
                      <Button
                        size="small"
                        variant="outlined"
                        key={item.id}
                        onClick={async () => {
                          const value = await api.chatAttachmentDownload(item.id);
                          if (value.downloadUrl)
                            window.open(value.downloadUrl, '_blank', 'noopener');
                        }}
                      >
                        Open {item.kind === 'MESSAGE_KIND_VIDEO' ? 'video' : 'image'}
                      </Button>
                    ))}
                    <Button
                      size="small"
                      variant="text"
                      onClick={() => api.reportChatMessage(message.id, 'Studio moderation review')}
                    >
                      Report
                    </Button>
                    {!message.withdrawnAt && (
                      <Button
                        size="small"
                        color="error"
                        variant="text"
                        onClick={() => socket.withdrawMessage(message.id)}
                      >
                        Remove message
                      </Button>
                    )}
                  </div>
                </article>
              ))}
            <form className="admin-form" onSubmit={send}>
              <input
                className="dh-file"
                aria-label="Chat image or video"
                type="file"
                accept="image/jpeg,image/png,image/webp,image/gif,video/mp4"
                onChange={async (event) => {
                  const file = event.target.files?.[0];
                  if (!file) return;
                  setUploadError(null);
                  setUploadStatus('uploading');
                  try {
                    const value = await api.uploadChatAttachment(selected, file, (progress) => {
                      setUploadProgress(progress);
                      if (progress === 100) setUploadStatus('scanning');
                    });
                    setAttachment({
                      id: value.id || '',
                      kind: file.type === 'video/mp4' ? 'MESSAGE_KIND_VIDEO' : 'MESSAGE_KIND_IMAGE',
                    });
                    setUploadStatus('ready');
                  } catch (error) {
                    setUploadProgress(0);
                    setUploadStatus('idle');
                    setUploadError(error);
                  }
                }}
              />
              {uploadStatus === 'uploading' && <span>Upload: {uploadProgress}%</span>}
              {uploadStatus === 'scanning' && <span>Security scan in progress…</span>}
              {uploadStatus === 'ready' && <span>Attachment ready</span>}
              <TextField
                value={body}
                onChange={(event) => setBody(event.target.value)}
                label="Reply as studio admin"
                inputProps={{ maxLength: 4000 }}
              />
              <Button
                type="submit"
                variant="contained"
                disabled={
                  !selected ||
                  !connected ||
                  uploadStatus === 'uploading' ||
                  uploadStatus === 'scanning' ||
                  (!body.trim() && !attachment)
                }
              >
                Send
              </Button>
            </form>
          </CardContent>
        </Card>
      </div>
    </Page>
  );
}

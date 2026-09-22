'use client';
import { type FormEvent, useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  api,
  chatFrameScope,
  ChatSocket,
  type ChatConversation,
  type ChatMessage,
} from '@dancehub/api-client';
import { useSearchParams } from 'next/navigation';
import { Empty, ErrorBox, Page } from '../workspace-shared';

export default function ChatPage({ studioId }: { studioId: string }) {
  const qc = useQueryClient();
  const params = useSearchParams();
  const groupStudio = params.get('studio_id') || studioId;
  const groupTeacher = params.get('teacher_id') || '';
  const [selected, setSelected] = useState(params.get('conversation') || '');
  const [body, setBody] = useState('');
  const [recipient, setRecipient] = useState('');
  const [connected, setConnected] = useState(false);
  const [uploadProgress, setUploadProgress] = useState(0);
  const [uploadStatus, setUploadStatus] = useState<'idle' | 'uploading' | 'scanning' | 'ready'>(
    'idle',
  );
  const [uploadError, setUploadError] = useState<unknown>(null);
  const [sendError, setSendError] = useState<unknown>(null);
  const [attachment, setAttachment] = useState<{ id: string; kind: string } | null>(null);
  const [replyTo, setReplyTo] = useState('');
  const conversations = useQuery({
    queryKey: ['chat-conversations'],
    queryFn: api.chatConversations,
  });
  const chatProfile = useQuery({ queryKey: ['profile'], queryFn: api.profile });
  const groups = useQuery({
    queryKey: ['chat-groups', groupStudio, groupTeacher],
    queryFn: () => api.chatGroups({ studio_id: groupStudio, teacher_id: groupTeacher }),
    enabled: !!groupStudio || !!groupTeacher,
  });
  const messages = useQuery({
    queryKey: ['chat-messages', selected],
    queryFn: () => api.chatMessages(selected),
    enabled: !!selected,
  });
  const direct = useMutation({
    mutationFn: () => api.createDirectConversation(recipient),
    onSuccess: (value) => {
      if (value.id) setSelected(value.id);
      qc.invalidateQueries({ queryKey: ['chat-conversations'] });
    },
  });
  const join = useMutation({
    mutationFn: (id: string) => api.joinChatGroup(id),
    onSuccess: (value) => {
      if (value.id) setSelected(value.id);
      qc.invalidateQueries({ queryKey: ['chat-conversations'] });
    },
  });
  const leave = useMutation({
    mutationFn: (id: string) => api.leaveChatGroup(id),
    onSuccess: () => {
      setSelected('');
      qc.invalidateQueries({ queryKey: ['chat-conversations'] });
      qc.invalidateQueries({ queryKey: ['chat-groups'] });
    },
  });
  const block = useMutation({
    mutationFn: ({ userId, active }: { userId: string; active: boolean }) =>
      active ? api.blockChatUser(userId) : api.unblockChatUser(userId),
  });
  const selectedConversation = conversations.data?.conversations?.find(
    (conversation) => conversation.id === selected,
  );
  const selectedGroup = groups.data?.groups?.find((group) => group.id === selected);
  const directTeacherId =
    selectedConversation?.kind === 'CONVERSATION_KIND_DIRECT_TEACHER'
      ? selectedConversation.teacherId || ''
      : '';
  const [socket] = useState(() => new ChatSocket());
  useEffect(() => {
    const last = (messages.data?.messages || []).reduce(
      (highest, message) => Math.max(highest, Number(message.sequence || 0)),
      0,
    );
    // The socket drops receipts that do not advance, so this cannot loop with the refresh.
    if (connected && selected) socket.markRead(selected, last);
  }, [connected, messages.data?.messages, selected, socket]);
  useEffect(() => {
    const refresh = () => {
      setConnected(true);
      qc.invalidateQueries({ queryKey: ['chat-messages', selected] });
      qc.invalidateQueries({ queryKey: ['chat-conversations'] });
    };
    const onFrame = (event: Event) => {
      const scope = chatFrameScope((event as CustomEvent).detail);
      if (scope === 'thread') refresh();
      else if (scope === 'conversations') {
        qc.invalidateQueries({ queryKey: ['chat-conversations'] });
      }
    };
    const disconnected = () => setConnected(false);
    socket.addEventListener('connected', refresh);
    socket.addEventListener('frame', onFrame);
    socket.addEventListener('disconnected', disconnected);
    void socket.connect();
    return () => {
      socket.removeEventListener('connected', refresh);
      socket.removeEventListener('frame', onFrame);
      socket.removeEventListener('disconnected', disconnected);
      socket.close();
    };
  }, [qc, selected, socket]);
  const send = (event: FormEvent) => {
    event.preventDefault();
    if (!selected || (!body.trim() && !attachment)) return;
    try {
      socket.sendMessage(
        selected,
        body.trim(),
        attachment ? [attachment.id] : [],
        attachment?.kind,
        replyTo,
      );
    } catch (error) {
      // Keep the draft: the socket is reconnecting and the message was not delivered.
      setSendError(error);
      return;
    }
    setSendError(null);
    setBody('');
    setAttachment(null);
    setUploadStatus('idle');
    setUploadProgress(0);
    setReplyTo('');
  };
  return (
    <Page title="Messages" subtitle={connected ? 'Connected' : 'Reconnecting…'}>
      <ErrorBox
        error={
          conversations.error ||
          groups.error ||
          messages.error ||
          direct.error ||
          join.error ||
          leave.error ||
          block.error ||
          uploadError ||
          sendError
        }
      />
      <form
        className="inline"
        onSubmit={(event) => {
          event.preventDefault();
          direct.mutate();
        }}
      >
        <input
          aria-label="Teacher account ID"
          value={recipient}
          onChange={(event) => setRecipient(event.target.value)}
          placeholder="Teacher account ID"
          required
        />
        <button className="dh-button" type="submit">
          Contact teacher
        </button>
      </form>
      <div className="chat-layout">
        <aside className="card">
          <h3>Conversations</h3>
          {conversations.data?.conversations?.map((conversation: ChatConversation) => (
            <button
              className="dh-choice"
              aria-pressed={selected === conversation.id}
              type="button"
              key={conversation.id}
              onClick={() => setSelected(conversation.id || '')}
            >
              {conversation.title || conversation.kind}{' '}
              <span>
                {Math.max(
                  0,
                  Number(conversation.lastSequence || 0) -
                    Number(conversation.lastReadSequence || 0),
                )}
              </span>
            </button>
          ))}
          <h3>Studio & teacher groups</h3>
          {groups.data?.groups?.map((group: ChatConversation) => (
            <button
              className="dh-choice"
              aria-pressed={selected === group.id}
              type="button"
              key={group.id}
              onClick={() =>
                group.id && (group.joined ? setSelected(group.id) : join.mutate(group.id))
              }
            >
              {group.title} · {group.memberCount || 0}
            </button>
          ))}
          {selectedGroup?.joined && (
            <button
              className="dh-button dh-button--danger"
              type="button"
              onClick={() => selectedGroup.id && leave.mutate(selectedGroup.id)}
            >
              Leave group
            </button>
          )}
          {directTeacherId && (
            <div className="dh-actions">
              <button
                className="dh-button dh-button--danger"
                type="button"
                onClick={() => block.mutate({ userId: directTeacherId, active: true })}
              >
                Block teacher
              </button>
              <button
                className="dh-button"
                type="button"
                onClick={() => block.mutate({ userId: directTeacherId, active: false })}
              >
                Unblock teacher
              </button>
            </div>
          )}
        </aside>
        <section className="card chat-thread" aria-live="polite">
          {(messages.data?.messages || [])
            .slice()
            .reverse()
            .map((message: ChatMessage) => (
              <article key={message.id}>
                <small>{message.senderDisplayName || message.senderId}</small>
                <p>{message.withdrawnAt ? 'Message withdrawn' : message.body}</p>
                {message.attachments?.map((item) => (
                  <button
                    className="dh-button"
                    type="button"
                    key={item.id}
                    onClick={async () => {
                      if (!item.id) return;
                      const download = await api.chatAttachmentDownload(item.id);
                      if (download.downloadUrl)
                        window.open(download.downloadUrl, '_blank', 'noopener');
                    }}
                  >
                    Open {item.kind === 'MESSAGE_KIND_VIDEO' ? 'video' : 'image'}
                  </button>
                ))}
                <time>{message.createdAt ? new Date(message.createdAt).toLocaleString() : ''}</time>
                {message.senderId === chatProfile.data?.id && !message.withdrawnAt && (
                  <div className="dh-actions">
                    <button
                      className="dh-button dh-button--ghost"
                      type="button"
                      onClick={() => {
                        const next = window.prompt('Edit message', message.body || '');
                        if (next) socket.editMessage(message.id || '', next);
                      }}
                    >
                      Edit
                    </button>
                    <button
                      className="dh-button dh-button--danger"
                      type="button"
                      onClick={() => socket.withdrawMessage(message.id || '')}
                    >
                      Withdraw
                    </button>
                  </div>
                )}
                {!message.withdrawnAt && (
                  <button
                    className="dh-button dh-button--ghost"
                    type="button"
                    onClick={() => setReplyTo(message.id || '')}
                  >
                    Reply
                  </button>
                )}
              </article>
            ))}
          {selected ? (
            <form onSubmit={send}>
              {replyTo && (
                <span>
                  Replying to {replyTo.slice(0, 8)}{' '}
                  <button
                    className="dh-button dh-button--ghost"
                    type="button"
                    onClick={() => setReplyTo('')}
                  >
                    Cancel
                  </button>
                </span>
              )}
              <input
                className="dh-file"
                type="file"
                aria-label="Chat image"
                accept="image/jpeg,image/png,image/webp,image/gif"
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
                    setAttachment({ id: value.id || '', kind: 'MESSAGE_KIND_IMAGE' });
                    setUploadStatus('ready');
                  } catch (error) {
                    setUploadStatus('idle');
                    setUploadProgress(0);
                    setUploadError(error);
                  }
                }}
              />
              {uploadProgress > 0 && uploadProgress < 100 && <span>{uploadProgress}%</span>}
              {uploadStatus === 'scanning' && <span>Security scan in progress…</span>}
              {uploadStatus === 'ready' && <span>Attachment ready</span>}
              <input
                value={body}
                maxLength={4000}
                onChange={(event) => {
                  setBody(event.target.value);
                  socket.typing(selected, Boolean(event.target.value));
                }}
                placeholder="Write a message"
              />
              <button
                className="dh-button dh-button--primary"
                type="submit"
                disabled={
                  !connected ||
                  uploadStatus === 'uploading' ||
                  uploadStatus === 'scanning' ||
                  (!body.trim() && !attachment)
                }
              >
                Send
              </button>
            </form>
          ) : (
            <Empty name="selected conversation" />
          )}
        </section>
      </div>
    </Page>
  );
}

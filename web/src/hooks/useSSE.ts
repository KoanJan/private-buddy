import { useEffect, useRef, useCallback } from 'react';
import { message } from 'antd';
import { useTranslation } from 'react-i18next';
import { MESSAGE_STATUS_COMPLETED } from '../types';
import type { Message } from '../types';
import { getDynamicApiBaseUrl } from '../services/api';
import { logger } from '../logger';

interface UseSSECallbacks {
  onMessage: (msg: Message) => void;
  onAgentStatus: (status: number, agentId?: number) => void;
}

/**
 * Manages a persistent SSE (Server-Sent Events) connection for a chat session.
 *
 * Returns { connect(sessionId), disconnect() }.
 * connect() closes any existing connection before opening a new one.
 * disconnect() is called automatically on unmount.
 *
 * SSE message types handled:
 * - "message"  → complete AI response
 * - "agent_status" → agent runtime status change
 * - "error"    → server-side error, shown to user
 */
export function useSSE(callbacks: UseSSECallbacks) {
  const { t } = useTranslation();
  const esRef = useRef<EventSource | null>(null);
  // Use ref for callbacks so the SSE handler always sees the latest
  const cbRef = useRef(callbacks);
  cbRef.current = callbacks;

  const disconnect = useCallback(() => {
    if (esRef.current) {
      logger.info('SSE: disconnecting');
      esRef.current.close();
      esRef.current = null;
    }
  }, []);

  const connect = useCallback(
    (sessionId: number) => {
      disconnect();

      const url = `${getDynamicApiBaseUrl()}/chat/stream/${sessionId}`;
      logger.info('SSE: connecting', url);

      const es = new EventSource(url);
      esRef.current = es;

      es.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data);

          if (data.type === 'message') {
            const newMsg: Message = {
              id: data.message_id,
              session_id: data.session_id || 0,
              person_id: 0, // caller fills this in
              content: data.content,
              status: MESSAGE_STATUS_COMPLETED,
              created_at: new Date().toISOString(),
              updated_at: new Date().toISOString(),
            };
            cbRef.current.onMessage(newMsg);
          } else if (data.type === 'agent_status') {
            cbRef.current.onAgentStatus(data.status as number, data.agent_id);
          } else if (data.type === 'error') {
            logger.error('SSE: server error', data.message);
            message.error(`${t('messages.aiResponseError')}: ${data.message}`);
          }
        } catch (err) {
          logger.error('SSE: parse error', err, event.data);
        }
      };

      es.onerror = (err) => {
        logger.error('SSE: connection error', err);
        disconnect();
      };
    },
    [disconnect, t],
  );

  // Cleanup on unmount
  useEffect(() => () => disconnect(), [disconnect]);

  return { connect, disconnect };
}

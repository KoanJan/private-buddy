import React, { useEffect, useState, useRef, useCallback } from 'react';
import { Input, Button, message, Spin } from 'antd';
import { RobotOutlined } from '@ant-design/icons';
import { Send, MessagesSquare, List } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { formatMessageTime } from '../utils/time';
import AgentAvatar from './AgentAvatar';
import AgentStatusBar from './AgentStatusBar';
import ActivityList from './ActivityList';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { Message, Session, Agent, SessionAgentStatus } from '../types';
import { MESSAGE_STATUS_COMPLETED, MESSAGE_ROLE_USER, MESSAGE_ROLE_ASSISTANT, PARTICIPANT_STATUS_IDLE, PARTICIPANT_STATUS_WORKING, TEMP_SESSION_ID } from '../types';
import { messageApi, sessionApi, agentApi, chatApi, getDynamicApiBaseUrl } from '../services/api';
import { logger } from '../logger';

/**
 * Props for the ChatWindow component.
 */
interface ChatWindowProps {
  session: Session | null;
  onSessionCreated?: (sessionId: number) => void;
}

/**
 * ChatWindow component handles the complete chat interface including:
 * - Message display with loading indicator for in-progress AI responses
 * - SSE (Server-Sent Events) connection management
 * - Session state transitions (temp to real)
 * 
 * Key state management:
 * - messages: Array of all messages in the session
 * - isStreaming: Whether SSE connection is active
 * - eventSourceRef: Reference to the current EventSource connection
 * 
 * SSE Flow:
 * 1. User sends message -> POST /chat/send creates user_msg + ai_msg placeholders
 * 2. Frontend connects to GET /chat/stream/{sessionId} via EventSource
 * 3. Server sends 'message' event with complete AI response
 * 4. 'done' event signals completion, frontend updates message status
 */
const ChatWindow: React.FC<ChatWindowProps> = ({ session, onSessionCreated }) => {
  const { t } = useTranslation();
  const [messages, setMessages] = useState<Message[]>([]);
  const [inputValue, setInputValue] = useState('');
  const [loading, setLoading] = useState(false);
  const [currentAgent, setCurrentAgent] = useState<Agent | null>(null);
  const [agentStatus, setAgentStatus] = useState<number>(PARTICIPANT_STATUS_IDLE);
  const [sessionAgents, setSessionAgents] = useState<SessionAgentStatus[]>([]);
  const [viewMode, setViewMode] = useState<'chat' | 'activity'>('chat');
  const [hasActivities, setHasActivities] = useState(false);
  
  // Derived: streaming state is determined by agent runtime status (from SSE agent_status events)
  const isStreaming = agentStatus !== PARTICIPANT_STATUS_IDLE;

  // Show activity toggle if the session has any work activities (checked via API).

  // Refs for managing async state and preventing race conditions
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const chatMessagesRef = useRef<HTMLDivElement>(null);
  const eventSourceRef = useRef<EventSource | null>(null);
  const prevSessionIdRef = useRef<number | null>(null);
  const currentLoadIdRef = useRef<number>(0);
  const isInitialLoadRef = useRef<boolean>(true);
  const skipLoadRef = useRef<boolean>(false);
  const loadMessagesRef = useRef<() => void>(() => {});

  const isTempSession = session?.id === TEMP_SESSION_ID;

  // Check whether the session has any activity records (existing message flag or API check).
  useEffect(() => {
    if (!session || isTempSession) {
      setHasActivities(false);
      return;
    }
    // Lightweight check: if any work has interactions, the activities API returns non-empty.
    sessionApi.getActivities(session.id).then(res => {
      setHasActivities(Array.isArray(res.data) && res.data.length > 0);
    }).catch(() => {
      setHasActivities(false);
    });
  }, [session?.id, isTempSession]);

  // Load session agents with their runtime status
  useEffect(() => {
    if (!session || isTempSession || !session.agent_id) {
      setSessionAgents([]);
      return;
    }

    const loadSessionAgents = async () => {
      try {
        const res = await chatApi.getSessionAgents(session.id);
        setSessionAgents(res.data);
      } catch (error) {
        logger.error('Failed to load session agents, falling back to current agent info', error, 'session_id', session?.id);
        // Fallback: construct from current agent info
        if (currentAgent) {
          setSessionAgents([{
            agent_id: currentAgent.id,
            name: currentAgent.name,
            avatar: currentAgent.avatar,
            status: PARTICIPANT_STATUS_IDLE,
          }]);
        }
      }
    };

    loadSessionAgents();
  }, [session?.id, session?.agent_id]);

  useEffect(() => {
    const loadAgent = async () => {
      if (!session || !session.agent_id) {
        setCurrentAgent(null);
        return;
      }
      
      try {
        const response = await agentApi.get(session.agent_id);
        setCurrentAgent(response.data);
      } catch (error) {
        logger.error('Failed to load agent:', error);
        setCurrentAgent(null);
      }
    };
    
    loadAgent();
  }, [session?.agent_id]);

  useEffect(() => {
    logger.debug('Messages updated:', messages.length, messages.map(m => ({ id: m.id, role: m.role, status: m.status, contentLength: m.content.length })));
  }, [messages]);

  /**
   * Handles session ID changes and manages EventSource lifecycle.
   * 
   * Special case: Temp session (id=-1) transitioning to real session
   * - When user sends first message in a new session, backend creates a real session
   * - Frontend receives the real session ID via SSE 'session_created' event
   * - We must preserve the streaming state and EventSource connection
   * - Skip loadMessages() to avoid overwriting the streaming UI
   * 
   * Normal case: Session switch or component unmount
   * - Close existing EventSource connection
   * - Reset streaming state
   * - Increment loadId to invalidate any pending loadMessages calls
   * - Clear messages and input
   */
  useEffect(() => {
    const prevId = prevSessionIdRef.current;
    const currentId = session?.id ?? null;
    
    // Check if this is a temp-to-real session transition
    const isTempToReal = prevId === -1 && currentId !== null && currentId !== -1;
    if (isTempToReal) {
      // Temp session transitioning to real: preserve streaming state and EventSource.
      // Messages are already set by handleSend, SSE is already connected.
      // Skip loadMessages to avoid overwriting the streaming UI.
      prevSessionIdRef.current = currentId;
      skipLoadRef.current = true;
      return;
    }
    
    // Normal session change: close EventSource and reset state
    if (eventSourceRef.current) {
      logger.info('Closing EventSource on session change');
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }
    
    currentLoadIdRef.current += 1;
    
    setMessages([]);
    isInitialLoadRef.current = true;
    setInputValue('');
    setViewMode('chat');
    
    prevSessionIdRef.current = currentId;
  }, [session?.id]);

  // Close EventSource on component unmount
  useEffect(() => {
    return () => {
      if (eventSourceRef.current) {
        logger.info('Closing EventSource on unmount');
        eventSourceRef.current.close();
        eventSourceRef.current = null;
      }
    };
  }, []);

  /**
   * Loads messages for the current session with race condition handling.
   * 
   * Race condition prevention:
   * - Uses currentLoadIdRef to track the latest load request
   * - If session changes while loading, stale responses are ignored
   * - Ensures UI always reflects the correct session's messages
   * 
   * SSE reconnection handling:
   * - If session status is STREAMING, looks for streaming message
   * - Reconnects to SSE stream with existing content
   * - Handles page refresh during streaming
   * 
   * @returns Promise<void>
   */
  const loadMessages = useCallback(async () => {
    if (!session || isTempSession) return;

    // Skip loading if this is a temp-to-real transition
    if (skipLoadRef.current) {
      skipLoadRef.current = false;
      return;
    }
    
    // Generate unique ID for this load request
    const loadId = ++currentLoadIdRef.current;
    logger.info('Loading messages for session:', session.id, 'loadId:', loadId);
    
    setLoading(true);
    try {
      const messagesRes = await messageApi.list(session.id);
      
      // Ignore stale responses from previous load requests
      if (loadId !== currentLoadIdRef.current) {
        logger.info('Stale loadMessages response ignored, loadId:', loadId);
        return;
      }
      
      logger.info('Messages loaded:', messagesRes.data.length);
      setMessages(messagesRes.data);
      
      // Always establish persistent SSE connection for this session.
      // The connection stays open to receive new messages as they arrive.
      if (!eventSourceRef.current) {
        connectToStream(session.id);
      }
    } catch (error) {
      logger.error('Failed to load messages:', error);
    } finally {
      if (loadId === currentLoadIdRef.current) {
        setLoading(false);
      }
    }
  }, [session, isTempSession]);

  useEffect(() => {
    loadMessagesRef.current = loadMessages;
  }, [loadMessages]);

  useEffect(() => {
    loadMessages();
  }, [loadMessages]);

  useEffect(() => {
    if (!chatMessagesRef.current) return;
    
    if (isInitialLoadRef.current && messages.length > 0) {
      chatMessagesRef.current.scrollTop = chatMessagesRef.current.scrollHeight;
      isInitialLoadRef.current = false;
    } else if (messages.length > 0) {
      chatMessagesRef.current.scrollTo({
        top: chatMessagesRef.current.scrollHeight,
        behavior: 'smooth'
      });
    }
  }, [messages]);

  /**
   * Establishes a persistent SSE connection for chat responses.
   * 
   * SSE Event Types:
   * - 'message': Complete AI response (message_id + content)
   * - 'error': Server-side error, displays error message
   * 
   * The connection stays open after receiving a message to support
   * continuous listening for new messages (e.g., multi-agent scenarios).
   * 
   * @param sessionId - The session ID to connect to
   */
  const connectToStream = (sessionId: number) => {
    // Close existing connection if any
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }
    
    const url = `${getDynamicApiBaseUrl()}/chat/stream/${sessionId}`;
    logger.info('Creating EventSource:', url);
    
    const eventSource = new EventSource(url);
    eventSourceRef.current = eventSource;
    
    eventSource.onopen = (event) => {
      logger.info('EventSource connection opened', event);
    };

    eventSource.onmessage = (event) => {
      try {
        logger.debug('SSE raw data:', event.data);
        const data = JSON.parse(event.data);
        logger.debug('SSE parsed message:', data);
        
        if (data.type === 'message') {
          // Received complete message from agent (committed from draft)
          logger.info('Received complete message, id:', data.message_id, 'content length:', data.content?.length);
          const newMsg: Message = {
            id: data.message_id,
            session_id: data.session_id || session?.id || 0,
            role: MESSAGE_ROLE_ASSISTANT,
            content: data.content,
            status: MESSAGE_STATUS_COMPLETED,
            created_at: new Date().toISOString(),
            updated_at: new Date().toISOString(),
          };
          setMessages(prev => [...prev, newMsg]);
        } else if (data.type === 'agent_status') {
          // Agent runtime status change
          logger.info('Agent status changed:', data.status);
          setAgentStatus(data.status);
          // Also update sessionAgents status
          if (data.agent_id) {
            setSessionAgents(prev => prev.map(a =>
              a.agent_id === data.agent_id ? { ...a, status: data.status as number } : a
            ));
          } else {
            logger.warn('agent_status event missing agent_id, cannot update sessionAgents', 'status', data.status);
          }
        } else if (data.type === 'error') {
          logger.error('SSE error from server:', data.message);
          message.error(`${t('messages.aiResponseError')}: ${data.message}`);
        }
      } catch (error) {
        logger.error('Failed to parse SSE message:', error, event.data);
      }
    };

    eventSource.onerror = (error) => {
      logger.error('SSE connection error:', error);
      // Only close and clear if this is still the current connection
      if (eventSourceRef.current === eventSource) {
        eventSource.close();
        eventSourceRef.current = null;
      }
    };
  };

  const handleSend = async () => {
    if (!inputValue.trim() || !session) return;

    logger.info('Sending message:', inputValue);

    try {
      if (isTempSession) {
        logger.info('Creating new session with agent_id:', session.agent_id);
        const response = await messageApi.createAndSend(
          inputValue,
          session.agent_id,
          inputValue.substring(0, 50)
        );
        
        const newSessionId = response.data.session_id;
        
        const userMessage: Message = {
          id: response.data.trigger_message_id,
          session_id: newSessionId,
          role: MESSAGE_ROLE_USER,
          content: inputValue,
          status: MESSAGE_STATUS_COMPLETED,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
        };

        // No placeholder AI message — agent response will arrive via SSE 'message' event
        // when the draft is committed
        setMessages([userMessage]);
        setInputValue('');
        setAgentStatus(PARTICIPANT_STATUS_WORKING);

        if (onSessionCreated) {
          onSessionCreated(newSessionId);
        }

        // Establish SSE connection for the new session
        connectToStream(newSessionId);
      } else {
        const response = await messageApi.send(session.id, inputValue);
        
        const userMessage: Message = {
          id: response.data.trigger_message_id,
          session_id: session.id,
          role: MESSAGE_ROLE_USER,
          content: inputValue,
          status: 1,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
        };
        
        // No placeholder AI message — agent response will arrive via SSE 'message' event
        setMessages(prev => [...prev, userMessage]);
        setInputValue('');
        setAgentStatus(PARTICIPANT_STATUS_WORKING);
        
        // Establish SSE connection if not already connected
        if (!eventSourceRef.current) {
          connectToStream(session.id);
        }
      }
    } catch (error) {
      logger.error('Failed to send message:', error);
      message.error(t('messages.sendError'));
    }
  };

  if (!session) {
    return (
      <div className="empty-state">
        <RobotOutlined className="empty-icon" />
        <div className="empty-text">{t('app.startNewChat')}</div>
        <div className="empty-hint">{t('app.selectOrCreate')}</div>
      </div>
    );
  }

  const isSendDisabled = !inputValue.trim();

  return (
    <>
      <div className="chat-header-row">
        <AgentStatusBar agents={sessionAgents} />
        {!isTempSession && hasActivities && (
          <button
            className={`chat-view-toggle ${viewMode === 'activity' ? 'active' : ''}`}
            onClick={() => setViewMode(viewMode === 'chat' ? 'activity' : 'chat')}
          >
            {viewMode === 'chat' ? <List size={14} /> : <MessagesSquare size={14} />}
            {viewMode === 'chat' ? t('chat.activityLog') : t('chat.chatView')}
          </button>
        )}
      </div>

      {viewMode === 'activity' ? (
        <ActivityList sessionId={session.id} agents={sessionAgents} />
      ) : (
        <>
          <div className="chat-messages" ref={chatMessagesRef}>
            {loading ? (
              <div style={{ textAlign: 'center', padding: '40px' }}>
                <Spin size="large" />
              </div>
            ) : (
              <>
                {messages.map((msg) => (
                    <div key={msg.id} className={`message-item ${msg.role === MESSAGE_ROLE_USER ? 'user' : 'assistant'}`}>
                      <div className="message-header">
                        {msg.role === MESSAGE_ROLE_USER ? (
                          <>
                            <span className="message-time">{formatMessageTime(new Date(msg.updated_at || msg.created_at))}</span>
                            <span className="message-role">{t('chat.me')}</span>
                          </>
                        ) : (
                          <>
                            <span className="message-role">
                              <AgentAvatar avatar={currentAgent?.avatar || ''} size={32} iconSize={16} borderRadius="8px" />
                              {currentAgent?.name || 'AI'}
                            </span>
                            <span className="message-time">{formatMessageTime(new Date(msg.updated_at || msg.created_at))}</span>
                          </>
                        )}
                      </div>
                      {msg.role === MESSAGE_ROLE_ASSISTANT && msg.content === '' && isStreaming ? (
                        <div style={{ textAlign: 'center', padding: '8px' }}>
                          <Spin size="small" />
                        </div>
                      ) : (
                      <div className="message-content">
                        {msg.role === MESSAGE_ROLE_ASSISTANT ? (
                          <ReactMarkdown remarkPlugins={[remarkGfm]}>
                            {msg.content}
                          </ReactMarkdown>
                        ) : (
                          msg.content
                        )}
                      </div>
                      )}
                    </div>
                  ))}
                <div ref={messagesEndRef} />
              </>
            )}
          </div>

          <div className="chat-input">
            <div className="input-container-wrapper">
              <div className="placeholder-text">{t('app.askAnything')}</div>
              <div className="input-container">
                <div className="input-area">
                  <Input.TextArea
                    placeholder=""
                    value={inputValue}
                    onChange={(e) => setInputValue(e.target.value)}
                    onPressEnter={(e) => {
                      if (!e.shiftKey) {
                        e.preventDefault();
                        handleSend();
                      }
                    }}
                    autoSize={{ minRows: 1, maxRows: 4 }}
                    bordered={false}
                    style={{
                      width: '100%',
                      fontSize: '14px',
                      resize: 'none',
                      backgroundColor: 'transparent'
                    }}
                  />
                </div>
                <div className="toolbar-area">
                  <Button
                    type="primary"
                    icon={<Send size={14} />}
                    onClick={handleSend}
                    disabled={isSendDisabled}
                    style={{
                      borderRadius: '50%',
                      width: '28px',
                      height: '28px',
                      padding: 0,
                      backgroundColor: isSendDisabled ? '#d1d5db' : '#1890ff',
                      borderColor: isSendDisabled ? '#d1d5db' : '#1890ff',
                      color: isSendDisabled ? 'var(--color-text-placeholder)' : '#ffffff',
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center'
                    }}
                  />
                </div>
              </div>
            </div>
          </div>
        </>
      )}

    </>
  );
};

export default ChatWindow;

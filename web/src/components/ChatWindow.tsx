import React, { useEffect, useState, useRef, useCallback } from 'react';
import { Input, Button, message, Spin } from 'antd';
import { RobotOutlined, ThunderboltOutlined } from '@ant-design/icons';
import { Send } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { formatMessageTime } from '../utils/time';
import AgentAvatar from './AgentAvatar';
import InteractionModal from './InteractionModal';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { Message, Session, Agent, Interaction } from '../types';
import { HAS_INTERACTIONS_PENDING, HAS_INTERACTIONS_EXISTS, MESSAGE_STATUS_COMPLETED } from '../types';
import { messageApi, agentApi, interactionApi, getDynamicApiBaseUrl } from '../services/api';
import { logger, MESSAGE_STATUS_STREAMING } from '../logger';

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
 * - Interaction polling for agent messages
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
 * 
 * Interaction Polling:
 * - Agent messages start with has_interactions=PENDING
 * - Frontend polls GET /interactions/{msgId}/status every 2s
 * - When status changes to EXISTS or NONE, polling stops
 */
const ChatWindow: React.FC<ChatWindowProps> = ({ session, onSessionCreated }) => {
  const { t } = useTranslation();
  const [messages, setMessages] = useState<Message[]>([]);
  const [inputValue, setInputValue] = useState('');
  const [loading, setLoading] = useState(false);
  const [currentAgent, setCurrentAgent] = useState<Agent | null>(null);
  const [interactionModalVisible, setInteractionModalVisible] = useState(false);
  const [selectedInteractions, setSelectedInteractions] = useState<Interaction[]>([]);
  const [interactionsLoading, setInteractionsLoading] = useState(false);
  
  // Derived: streaming state is determined by whether any assistant message is still streaming
  const isStreaming = messages.some(m => m.role === 'assistant' && m.status === MESSAGE_STATUS_STREAMING);

  // Refs for managing async state and preventing race conditions
  const pollingRef = useRef<Map<number, ReturnType<typeof setInterval>>>(new Map());
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const chatMessagesRef = useRef<HTMLDivElement>(null);
  const eventSourceRef = useRef<EventSource | null>(null);
  const prevSessionIdRef = useRef<number | null>(null);
  const currentLoadIdRef = useRef<number>(0);
  const isInitialLoadRef = useRef<boolean>(true);
  const skipLoadRef = useRef<boolean>(false);
  const loadMessagesRef = useRef<() => void>(() => {});

  const isTempSession = session?.id === -1;

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

  /**
   * Starts polling for interaction status on an agent message.
   * 
   * Interaction status values:
   * - PENDING (0): Agent is still processing, continue polling
   * - EXISTS (1): Agent has interactions, show view button
   * - NONE (2): Agent has no interactions, hide interaction UI
   * 
   * Polling stops automatically when status changes from PENDING.
   * 
   * @param aiMessageId - The agent message ID to poll
   */
  const startPolling = (aiMessageId: number) => {
    // Avoid duplicate polling for the same message
    if (pollingRef.current.has(aiMessageId)) return;

    const timer = setInterval(async () => {
      try {
        const res = await interactionApi.getInteractionStatus(aiMessageId);
        const status = res.data.has_interactions;
        // Stop polling when status is no longer PENDING
        if (status !== HAS_INTERACTIONS_PENDING) {
          setMessages(prev => prev.map(m =>
            m.id === aiMessageId ? { ...m, has_interactions: status } : m
          ));
          clearInterval(timer);
          pollingRef.current.delete(aiMessageId);
        }
      } catch (err) {
        logger.error('Failed to poll interaction status:', err);
      }
    }, 2000); // Poll every 2 seconds
    pollingRef.current.set(aiMessageId, timer);
  };

  // Poll for interaction status on agent messages with has_interactions=PENDING
  useEffect(() => {
    const pendingAgentMessages = messages.filter(
      m => m.role === 'assistant' && m.has_interactions === HAS_INTERACTIONS_PENDING
    );

    pendingAgentMessages.forEach(msg => {
      startPolling(msg.id);
    });

    return () => {
      pollingRef.current.forEach(timer => clearInterval(timer));
      pollingRef.current.clear();
    };
  }, [messages]);

  const handleViewInteractions = async (agentMsgId: number) => {
    setInteractionsLoading(true);
    setInteractionModalVisible(true);
    try {
      const res = await interactionApi.getInteractions(agentMsgId);
      setSelectedInteractions(res.data.interactions);
    } catch (err) {
      logger.error('Failed to load interactions:', err);
      message.error('Failed to load interaction records');
    } finally {
      setInteractionsLoading(false);
    }
  };

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
          // Received complete message, update message list
          logger.info('Received complete message, id:', data.message_id, 'content length:', data.content?.length);
          setMessages(prev => prev.map(m => {
            if (m.id === data.message_id) {
              return { ...m, content: data.content, status: MESSAGE_STATUS_COMPLETED };
            }
            return m;
          }));
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
    if (!inputValue.trim() || !session || isStreaming) return;

    logger.info('Sending message:', inputValue);

    try {
      if (isTempSession) {
        const response = await messageApi.createAndSend(
          inputValue,
          session.agent_id,
          inputValue.substring(0, 50)
        );
        
        const newSessionId = response.data.session_id;
        const aiMessageId = response.data.ai_message_id;
        
        const userMessage: Message = {
          id: response.data.trigger_message_id,
          session_id: newSessionId,
          role: 'user',
          content: inputValue,
          status: 1,
          has_interactions: 2,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
        };
        
        const aiMessage: Message = {
          id: aiMessageId,
          session_id: newSessionId,
          role: 'assistant',
          content: '',
          status: 0,
          has_interactions: 0,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
        };
        
        setMessages([userMessage, aiMessage]);
        setInputValue('');
        
        startPolling(aiMessageId);

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
          role: 'user',
          content: inputValue,
          status: 1,
          has_interactions: 2,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
        };
        
        const aiMessageId = response.data.ai_message_id;
        const aiMessage: Message = {
          id: aiMessageId,
          session_id: session.id,
          role: 'assistant',
          content: '',
          status: 0,
          has_interactions: 0,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
        };
        
        setMessages(prev => [...prev, userMessage, aiMessage]);
        setInputValue('');
        
        startPolling(aiMessageId);
        
        // Establish SSE connection if not already connected
        if (!eventSourceRef.current) {
          connectToStream(session.id);
        }
      }
    } catch (error: any) {
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

  const isSendDisabled = !inputValue.trim() || isStreaming;

  return (
    <>
      <div className="chat-messages" ref={chatMessagesRef}>
        {loading ? (
          <div style={{ textAlign: 'center', padding: '40px' }}>
            <Spin size="large" />
          </div>
        ) : (
          <>
            {messages.map((msg) => (
                <div key={msg.id} className={`message-item ${msg.role}`}>
                  <div className="message-header">
                    {msg.role === 'user' ? (
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
                  {msg.role === 'assistant' && msg.status === MESSAGE_STATUS_STREAMING ? (
                    <div style={{ textAlign: 'center', padding: '8px' }}>
                      <Spin size="small" />
                    </div>
                  ) : (
                  <div className="message-content">
                    {msg.role === 'assistant' ? (
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>
                        {msg.content}
                      </ReactMarkdown>
                    ) : (
                      msg.content
                    )}
                  </div>
                  )}
                  {msg.role === 'assistant' && msg.has_interactions === HAS_INTERACTIONS_EXISTS && (
                    <div style={{ marginTop: '4px' }}>
                      <Button
                        type="text"
                        size="small"
                        icon={<ThunderboltOutlined />}
                        onClick={() => handleViewInteractions(msg.id)}
                        style={{ color: '#8b5cf6', fontSize: '12px', padding: '0 4px' }}
                      >
                        Interactions
                      </Button>
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
                placeholder={isStreaming ? t('app.generating') : ""}
                value={inputValue}
                onChange={(e) => setInputValue(e.target.value)}
                onPressEnter={(e) => {
                  if (!e.shiftKey) {
                    e.preventDefault();
                    handleSend();
                  }
                }}
                autoSize={{ minRows: 1, maxRows: 4 }}
                disabled={isStreaming}
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

      <InteractionModal
        visible={interactionModalVisible}
        loading={interactionsLoading}
        interactions={selectedInteractions}
        onClose={() => {
          setInteractionModalVisible(false);
          setSelectedInteractions([]);
        }}
      />
    </>
  );
};

export default ChatWindow;

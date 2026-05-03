import React, { useEffect, useState, useRef, useCallback } from 'react';
import { Input, Button, message, Spin, Modal } from 'antd';
import { RobotOutlined, ThunderboltOutlined } from '@ant-design/icons';
import { Send } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { formatMessageTime } from '../utils/time';
import LoadingSpinner from './LoadingSpinner';
import AgentAvatar from './AgentAvatar';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { Message, Session, Agent, Interaction } from '../types';
import { HAS_INTERACTIONS_PENDING, HAS_INTERACTIONS_EXISTS, INTERACTION_TYPE_REQUEST } from '../types';
import { messageApi, sessionApi, agentApi, interactionApi, getDynamicApiBaseUrl } from '../services/api';
import { logger, MESSAGE_STATUS_STREAMING, SESSION_STATUS_STREAMING } from '../logger';

interface ChatWindowProps {
  session: Session | null;
  onSessionCreated?: (sessionId: number) => void;
}

const ChatWindow: React.FC<ChatWindowProps> = ({ session, onSessionCreated }) => {
  const { t } = useTranslation();
  const [messages, setMessages] = useState<Message[]>([]);
  const [inputValue, setInputValue] = useState('');
  const [loading, setLoading] = useState(false);
  const [streamingMessage, setStreamingMessage] = useState('');
  const [isStreaming, setIsStreaming] = useState(false);
  const [currentAgent, setCurrentAgent] = useState<Agent | null>(null);
  const [interactionModalVisible, setInteractionModalVisible] = useState(false);
  const [selectedInteractions, setSelectedInteractions] = useState<Interaction[]>([]);
  const [interactionsLoading, setInteractionsLoading] = useState(false);
  const pollingRef = useRef<Map<number, ReturnType<typeof setInterval>>>(new Map());
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const chatMessagesRef = useRef<HTMLDivElement>(null);
  const eventSourceRef = useRef<EventSource | null>(null);
  const prevSessionIdRef = useRef<number | null>(null);
  const currentLoadIdRef = useRef<number>(0);
  const isInitialLoadRef = useRef<boolean>(true);

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

  useEffect(() => {
    const prevId = prevSessionIdRef.current;
    const currentId = session?.id ?? null;
    
    if (eventSourceRef.current) {
      logger.info('Closing EventSource on session change');
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }
    
    setIsStreaming(false);
    currentLoadIdRef.current += 1;
    
    const isTempToReal = prevId === -1 && currentId !== null && currentId !== -1;
    if (!isTempToReal) {
      setMessages([]);
      setStreamingMessage('');
      isInitialLoadRef.current = true;
    }
    setInputValue('');
    
    prevSessionIdRef.current = currentId;
    
    return () => {
      if (eventSourceRef.current) {
        logger.info('Closing EventSource on unmount');
        eventSourceRef.current.close();
        eventSourceRef.current = null;
      }
    };
  }, [session?.id]);

  const loadMessages = useCallback(async () => {
    if (!session || isTempSession) return;
    
    const loadId = ++currentLoadIdRef.current;
    logger.info('Loading messages for session:', session.id, 'loadId:', loadId);
    
    setLoading(true);
    try {
      const [messagesRes, sessionRes] = await Promise.all([
        messageApi.list(session.id),
        sessionApi.get(session.id)
      ]);
      
      if (loadId !== currentLoadIdRef.current) {
        logger.info('Stale loadMessages response ignored, loadId:', loadId);
        return;
      }
      
      logger.info('Messages loaded:', messagesRes.data.length, 'Session status:', sessionRes.data.status);
      setMessages(messagesRes.data);
      
      if (sessionRes.data.status === SESSION_STATUS_STREAMING) {
        const streamingMsg = messagesRes.data.find(m => m.status === MESSAGE_STATUS_STREAMING);
        if (streamingMsg) {
          logger.info('Found streaming message, reconnecting...', streamingMsg.id, 'content length:', streamingMsg.content.length);
          setStreamingMessage(streamingMsg.content);
          setIsStreaming(true);
          connectToStream(session.id, loadId);
        } else {
          logger.warn('Session is streaming but no streaming message found');
          setIsStreaming(false);
        }
      } else {
        setIsStreaming(false);
      }
    } catch (error) {
      logger.error('Failed to load messages:', error);
      if (loadId === currentLoadIdRef.current) {
        setIsStreaming(false);
      }
    } finally {
      if (loadId === currentLoadIdRef.current) {
        setLoading(false);
      }
    }
  }, [session, isTempSession]);

  useEffect(() => {
    loadMessages();
  }, [loadMessages]);

  // Poll for interaction status on agent messages with has_interactions=PENDING
  useEffect(() => {
    const pendingAgentMessages = messages.filter(
      m => m.role === 'assistant' && m.has_interactions === HAS_INTERACTIONS_PENDING
    );

    pendingAgentMessages.forEach(msg => {
      if (!pollingRef.current.has(msg.id)) {
        const timer = setInterval(async () => {
          try {
            const res = await interactionApi.getInteractionStatus(msg.id);
            const status = res.data.has_interactions;
            if (status !== HAS_INTERACTIONS_PENDING) {
              setMessages(prev => prev.map(m =>
                m.id === msg.id ? { ...m, has_interactions: status } : m
              ));
              clearInterval(timer);
              pollingRef.current.delete(msg.id);
            }
          } catch (err) {
            logger.error('Failed to poll interaction status:', err);
          }
        }, 2000);
        pollingRef.current.set(msg.id, timer);
      }
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

  useEffect(() => {
    if (!chatMessagesRef.current || !streamingMessage) return;
    
    chatMessagesRef.current.scrollTop = chatMessagesRef.current.scrollHeight;
  }, [streamingMessage]);

  const connectToStream = (sessionId: number, loadId?: number) => {
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
        
        if (data.type === 'existing') {
          logger.info('Received existing content:', data.content.length, 'chars');
          setStreamingMessage(data.content);
        } else if (data.type === 'chunk') {
          setStreamingMessage(prev => prev + data.content);
        } else if (data.type === 'done') {
          logger.info('SSE stream completed');
          setIsStreaming(false);
          loadMessages();
          setStreamingMessage('');
          eventSource.close();
          eventSourceRef.current = null;
        } else if (data.type === 'error') {
          logger.error('SSE error from server:', data.message);
          message.error(`${t('messages.aiResponseError')}: ${data.message}`);
          setIsStreaming(false);
          setStreamingMessage('');
          eventSource.close();
          eventSourceRef.current = null;
        }
      } catch (error) {
        logger.error('Failed to parse SSE message:', error, event.data);
      }
    };

    eventSource.onerror = (error) => {
      logger.error('SSE connection error:', error);
      if (loadId === undefined || loadId === currentLoadIdRef.current) {
        setIsStreaming(false);
      }
      eventSource.close();
      eventSourceRef.current = null;
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
        
        setMessages([userMessage]);
        setInputValue('');
        setStreamingMessage('');
        setIsStreaming(true);
        
        if (onSessionCreated) {
          onSessionCreated(newSessionId);
        }
      } else {
        const userMessage: Message = {
          id: Date.now(),
          session_id: session.id,
          role: 'user',
          content: inputValue,
          status: 1,
          has_interactions: 2,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
        };
        
        setMessages(prev => [...prev, userMessage]);
        setInputValue('');
        setStreamingMessage('');
        setIsStreaming(true);
        
        await messageApi.send(session.id, inputValue);
        
        const [messagesRes, sessionRes] = await Promise.all([
          messageApi.list(session.id),
          sessionApi.get(session.id)
        ]);
        
        setMessages(messagesRes.data);
        
        if (sessionRes.data.status === SESSION_STATUS_STREAMING) {
          connectToStream(session.id);
        } else {
          setIsStreaming(false);
        }
      }
    } catch (error: any) {
      logger.error('Failed to send message:', error);
      message.error(t('messages.sendError'));
      setIsStreaming(false);
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
            {messages
              .filter(msg => !(msg.role === 'assistant' && msg.status === MESSAGE_STATUS_STREAMING))
              .map((msg) => (
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
                  <div className="message-content">
                    {msg.role === 'assistant' ? (
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>
                        {msg.content}
                      </ReactMarkdown>
                    ) : (
                      msg.content
                    )}
                  </div>
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
            {isStreaming && (
              <div className="message-item assistant">
                <div className="message-header">
                  <span className="message-role">
                    <AgentAvatar avatar={currentAgent?.avatar || ''} size={32} iconSize={16} borderRadius="8px" />
                    {currentAgent?.name || 'AI'}
                  </span>
                  <span className="message-time">
                    <LoadingSpinner />
                  </span>
                </div>
                <div className="message-content">
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>
                    {streamingMessage}
                  </ReactMarkdown>
                </div>
              </div>
            )}
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

      <Modal
        title="Interaction Records"
        open={interactionModalVisible}
        onCancel={() => {
          setInteractionModalVisible(false);
          setSelectedInteractions([]);
        }}
        footer={null}
        width={800}
      >
        {interactionsLoading ? (
          <div style={{ textAlign: 'center', padding: '20px' }}>
            <Spin />
          </div>
        ) : (
          <div style={{ maxHeight: '600px', overflowY: 'auto' }}>
            {selectedInteractions.length === 0 ? (
              <div style={{ textAlign: 'center', color: 'var(--color-text-placeholder)', padding: '20px' }}>
                No interaction records found
              </div>
            ) : (
              (() => {
                const iterationMap = new Map<number, Interaction[]>();
                selectedInteractions.forEach(interaction => {
                  const list = iterationMap.get(interaction.iteration) || [];
                  list.push(interaction);
                  iterationMap.set(interaction.iteration, list);
                });
                return Array.from(iterationMap.entries()).map(([iteration, items]) => (
                  <div key={`iter-${iteration}`} style={{ marginBottom: '16px' }}>
                    <div style={{ fontWeight: 600, fontSize: '14px', marginBottom: '8px', color: '#374151' }}>
                      Iteration {iteration}
                    </div>
                    {items.map(interaction => {
                      const typeLabel = interaction.type === INTERACTION_TYPE_REQUEST ? 'Request' : 'Response';
                      const typeColor = interaction.type === INTERACTION_TYPE_REQUEST ? '#3b82f6' : '#10b981';
                      return (
                        <div key={`interaction-${interaction.id}`} style={{ marginLeft: '16px', marginBottom: '8px' }}>
                          <div style={{ fontSize: '12px', marginBottom: '4px' }}>
                            <span style={{ color: typeColor, fontWeight: 500 }}>{typeLabel}</span>
                            <span style={{ color: 'var(--color-text-placeholder)', marginLeft: '8px' }}>
                              {formatMessageTime(new Date(interaction.updated_at))}
                            </span>
                          </div>
                          <pre style={{
                            whiteSpace: 'pre-wrap',
                            wordBreak: 'break-word',
                            fontSize: '12px',
                            lineHeight: '1.5',
                            margin: 0,
                            padding: '8px',
                            background: '#f9fafb',
                            borderRadius: '4px',
                            border: '1px solid #e5e7eb',
                            maxHeight: '300px',
                            overflowY: 'auto',
                          }}>
                            {(() => {
                              try {
                                return JSON.stringify(JSON.parse(interaction.data), null, 2);
                              } catch {
                                return interaction.data;
                              }
                            })()}
                          </pre>
                        </div>
                      );
                    })}
                  </div>
                ));
              })()
            )}
          </div>
        )}
      </Modal>
    </>
  );
};

export default ChatWindow;

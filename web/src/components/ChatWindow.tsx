import React, { useEffect, useState, useRef } from 'react';
import { Input, Button, message, Spin } from 'antd';
import { RobotOutlined } from '@ant-design/icons';
import { Send } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { Message, Session, Agent } from '../types';
import { messageApi, sessionApi, agentApi } from '../services/api';
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
  const [currentAgent, setCurrentAgent] = useState<Agent | null>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const eventSourceRef = useRef<EventSource | null>(null);

  const isSessionStreaming = session?.status === SESSION_STATUS_STREAMING;
  const isTempSession = session?.id === -1;

  useEffect(() => {
    const loadAgent = async () => {
      if (!session || isTempSession) {
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
    if (eventSourceRef.current) {
      logger.info('Closing EventSource on session change');
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }
    setMessages([]);
    setStreamingMessage('');
    setInputValue('');
  }, [session?.id]);

  const loadMessages = async () => {
    if (!session || isTempSession) return;
    
    logger.info('Loading messages for session:', session.id);
    setLoading(true);
    try {
      const [messagesRes, sessionRes] = await Promise.all([
        messageApi.list(session.id),
        sessionApi.get(session.id)
      ]);
      
      logger.info('Messages loaded:', messagesRes.data.length, 'Session status:', sessionRes.data.status);
      setMessages(messagesRes.data);
      
      if (sessionRes.data.status === SESSION_STATUS_STREAMING) {
        const streamingMsg = messagesRes.data.find(m => m.status === MESSAGE_STATUS_STREAMING);
        if (streamingMsg) {
          logger.info('Found streaming message, reconnecting...', streamingMsg.id, 'content length:', streamingMsg.content.length);
          setStreamingMessage(streamingMsg.content);
          connectToStream(session.id);
        } else {
          logger.warn('Session is streaming but no streaming message found');
        }
      }
    } catch (error) {
      logger.error('Failed to load messages:', error);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadMessages();
  }, [session?.id]);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages, streamingMessage]);

  const connectToStream = (sessionId: number) => {
    const url = `http://localhost:8000/api/chat/stream/${sessionId}`;
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
          loadMessages();
          setStreamingMessage('');
          eventSource.close();
          eventSourceRef.current = null;
        } else if (data.type === 'error') {
          logger.error('SSE error from server:', data.message);
          message.error(`${t('messages.aiResponseError')}: ${data.message}`);
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
      eventSource.close();
      eventSourceRef.current = null;
    };
  };

  const handleSend = async () => {
    if (!inputValue.trim() || !session || isSessionStreaming) return;

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
          id: response.data.user_message_id,
          session_id: newSessionId,
          role: 'user',
          content: inputValue,
          status: 1,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
        };
        
        setMessages([userMessage]);
        setInputValue('');
        setStreamingMessage('');
        
        if (onSessionCreated) {
          onSessionCreated(newSessionId);
        }
        
        connectToStream(newSessionId);
      } else {
        await messageApi.send(session.id, inputValue);
        
        const userMessage: Message = {
          id: Date.now(),
          session_id: session.id,
          role: 'user',
          content: inputValue,
          status: 1,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
        };
        
        setMessages(prev => [...prev, userMessage]);
        setInputValue('');
        setStreamingMessage('');
        
        connectToStream(session.id);
      }
    } catch (error: any) {
      logger.error('Failed to send message:', error);
      if (error.response?.data?.detail) {
        message.error(error.response.data.detail);
      } else {
        message.error(t('messages.sendFailed'));
      }
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

  const isSendDisabled = !inputValue.trim() || isSessionStreaming;

  return (
    <>
      <div className="chat-messages">
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
                        <span className="message-time">{msg.updated_at ? new Date(msg.updated_at).toLocaleTimeString() : new Date(msg.created_at).toLocaleTimeString()}</span>
                        <span className="message-role">{t('chat.me')}</span>
                      </>
                    ) : (
                      <>
                        <span className="message-role">{currentAgent?.name || 'AI'}</span>
                        <span className="message-time">{msg.updated_at ? new Date(msg.updated_at).toLocaleTimeString() : new Date(msg.created_at).toLocaleTimeString()}</span>
                      </>
                    )}
                  </div>
                  <div className="message-content">
                    {msg.content}
                  </div>
                </div>
              ))}
            {isSessionStreaming && (
              <div className="message-item assistant">
                <div className="message-header">
                  <span className="message-role">{currentAgent?.name || 'AI'}</span>
                  <span className="message-time">
                    <span className="typing-dots">
                      <span>.</span><span>.</span><span>.</span>
                    </span>
                  </span>
                </div>
                <div className="message-content">
                  {streamingMessage}
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
                placeholder={isSessionStreaming ? t('app.generating') : ""}
                value={inputValue}
                onChange={(e) => setInputValue(e.target.value)}
                onPressEnter={(e) => {
                  if (!e.shiftKey) {
                    e.preventDefault();
                    handleSend();
                  }
                }}
                autoSize={{ minRows: 1, maxRows: 4 }}
                disabled={isSessionStreaming}
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
                  color: isSendDisabled ? '#9ca3af' : '#ffffff',
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
  );
};

export default ChatWindow;
import React, { useEffect, useState, useRef } from 'react';
import { Input, Button, Spin } from 'antd';
import { RobotOutlined } from '@ant-design/icons';
import { Send } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { formatMessageTime } from '../utils/time';
import AgentAvatar from './AgentAvatar';
import AgentStatusBar from './AgentStatusBar';
import ActivityList from './ActivityList';
import ReceivedPanel from './ReceivedPanel';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { useSSE } from '../hooks/useSSE';
import { useMessages } from '../hooks/useMessages';
import type { Message, Session, Agent, SessionAgentStatus } from '../types';
import { PARTICIPANT_STATUS_IDLE, PARTICIPANT_STATUS_WORKING, TEMP_SESSION_ID } from '../types';
import { agentApi, chatApi, personApi } from '../services/api';
import { logger } from '../logger';

interface ChatWindowProps {
  session: Session | null;
  onSessionCreated?: (sessionId: number) => void;
}

const ChatWindow: React.FC<ChatWindowProps> = ({ session, onSessionCreated }) => {
  const { t } = useTranslation();
  const [inputValue, setInputValue] = useState('');
  const [currentAgent, setCurrentAgent] = useState<Agent | null>(null);
  const [agentStatus, setAgentStatus] = useState<number>(PARTICIPANT_STATUS_IDLE);
  const [sessionAgents, setSessionAgents] = useState<SessionAgentStatus[]>([]);
  const [viewMode, setViewMode] = useState<'chat' | 'activity' | 'received'>('chat');
  const [currentUserPersonId, setCurrentUserPersonId] = useState<number>(0);
  const tabContainerRef = useRef<HTMLDivElement>(null);
  const tabRefs = useRef<Record<string, HTMLButtonElement | null>>({});
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const chatMessagesRef = useRef<HTMLDivElement>(null);
  const isInitialLoadRef = useRef<boolean>(true);

  const isTempSession = session?.id === TEMP_SESSION_ID;
  const isStreaming = agentStatus !== PARTICIPANT_STATUS_IDLE;

  // ---- Hooks: SSE connection ----

  const { connect: sseConnect, disconnect: sseDisconnect } = useSSE({
    onMessage: (msg: Message) => {
      // Fill in agent's person_id from currentAgent before appending
      if (currentAgent?.person_id) {
        msg.person_id = currentAgent.person_id;
      }
      setMessages(prev => [...prev, msg]);
    },
    onAgentStatus: (status: number, agentId?: number) => {
      setAgentStatus(status);
      if (agentId) {
        setSessionAgents(prev =>
          prev.map(a => (a.agent_id === agentId ? { ...a, status } : a)),
        );
      }
    },
  });

  // ---- Hooks: Messages ----

  const {
    messages,
    setMessages,
    loading: messagesLoading,
    markTempToRealTransition,
    handleSend: sendMessage,
  } = useMessages(session);

  // ---- Data loading ----

  // Load session agents
  useEffect(() => {
    if (!session || isTempSession || !session.agent_id) {
      setSessionAgents([]);
      return;
    }

    const load = async () => {
      try {
        const res = await chatApi.getSessionAgents(session.id);
        setSessionAgents(res.data);
      } catch (error) {
        logger.error('Failed to load session agents', error, 'session_id', session.id);
        if (currentAgent) {
          setSessionAgents([
            { agent_id: currentAgent.id, name: currentAgent.name, avatar: currentAgent.avatar, status: PARTICIPANT_STATUS_IDLE },
          ]);
        }
      }
    };
    load();
  }, [session?.id, session?.agent_id]);

  // Load agent
  useEffect(() => {
    if (!session?.agent_id) {
      setCurrentAgent(null);
      return;
    }
    agentApi.get(session.agent_id)
      .then(res => setCurrentAgent(res.data))
      .catch(err => logger.error('Failed to load agent:', err));
  }, [session?.agent_id]);

  // Load current user
  useEffect(() => {
    personApi.me()
      .then(res => setCurrentUserPersonId(res.data.id))
      .catch(err => logger.error('Failed to load current user person', err));
  }, []);

  // Scroll to bottom on new messages
  useEffect(() => {
    if (!chatMessagesRef.current) return;
    if (isInitialLoadRef.current && messages.length > 0) {
      chatMessagesRef.current.scrollTop = chatMessagesRef.current.scrollHeight;
      isInitialLoadRef.current = false;
    } else if (messages.length > 0) {
      chatMessagesRef.current.scrollTo({
        top: chatMessagesRef.current.scrollHeight,
        behavior: 'smooth',
      });
    }
  }, [messages]);

  // Cleanup SSE on unmount (useSSE handles this, but explicit for session transitions)
  useEffect(() => {
    return () => sseDisconnect();
  }, []);

  // Tab indicator position
  useEffect(() => {
    const container = tabContainerRef.current;
    const activeTab = tabRefs.current[viewMode];
    if (!container || !activeTab) return;
    const containerRect = container.getBoundingClientRect();
    const tabRect = activeTab.getBoundingClientRect();
    container.style.setProperty('--indicator-left', `${tabRect.left - containerRect.left}px`);
    container.style.setProperty('--indicator-width', `${tabRect.width}px`);
  }, [viewMode]);

  // ---- Handlers ----

  const handleSend = async () => {
    if (!inputValue.trim() || !session) return;

    const result = await sendMessage(inputValue, session, currentUserPersonId);
    if (!result) return;

    setInputValue('');
    setAgentStatus(PARTICIPANT_STATUS_WORKING);

    if (result.sessionId !== session.id && onSessionCreated) {
      // Temp→real transition
      markTempToRealTransition();
      onSessionCreated(result.sessionId);
    }

    // Always connect SSE (will be a no-op if already connected to same session)
    sseConnect(result.sessionId);
  };

  // ---- Render ----

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
        {!isTempSession && (
          <div className="chat-view-tabs" ref={tabContainerRef}>
            <div className="chat-tab-indicator" />
            <button
              ref={el => { tabRefs.current.chat = el; }}
              className={`chat-tab ${viewMode === 'chat' ? 'active' : ''}`}
              onClick={() => setViewMode('chat')}
            >
              {t('viewTabs.chat')}
            </button>
            <button
              ref={el => { tabRefs.current.activity = el; }}
              className={`chat-tab ${viewMode === 'activity' ? 'active' : ''}`}
              onClick={() => setViewMode('activity')}
            >
              {t('viewTabs.activities')}
            </button>
            <button
              ref={el => { tabRefs.current.received = el; }}
              className={`chat-tab ${viewMode === 'received' ? 'active' : ''}`}
              onClick={() => setViewMode('received')}
            >
              {t('viewTabs.received')}
            </button>
          </div>
        )}
      </div>

      {viewMode === 'activity' ? (
        <ActivityList sessionId={session.id} agents={sessionAgents} />
      ) : viewMode === 'received' ? (
        <ReceivedPanel sessionId={session.id} />
      ) : (
        <>
          <div className="chat-messages" ref={chatMessagesRef}>
            {messagesLoading ? (
              <div style={{ textAlign: 'center', padding: '40px' }}>
                <Spin size="large" />
              </div>
            ) : (
              <>
                {messages.map(msg => (
                  <div key={msg.id} className={`message-item ${msg.person_id === currentUserPersonId ? 'user' : 'assistant'}`}>
                    <div className="message-header">
                      {msg.person_id === currentUserPersonId ? (
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
                    {msg.person_id !== currentUserPersonId && msg.content === '' && isStreaming ? (
                      <div style={{ textAlign: 'center', padding: '8px' }}>
                        <Spin size="small" />
                      </div>
                    ) : (
                      <div className="message-content">
                        {msg.person_id !== currentUserPersonId ? (
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
                    onChange={e => setInputValue(e.target.value)}
                    onPressEnter={e => {
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
                      backgroundColor: 'transparent',
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
                      justifyContent: 'center',
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

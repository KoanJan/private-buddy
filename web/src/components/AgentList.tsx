import React, { useEffect, useState } from 'react';
import { Button, Modal, message, Collapse, Input } from 'antd';
import { DeleteOutlined, MessageOutlined, RobotOutlined, EditOutlined } from '@ant-design/icons';
import { MessageCircle } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { AgentWithSessions, Session, SessionBrief } from '../types';
import { agentApi, sessionApi } from '../services/api';
import { logger } from '../logger';
import { confirmDelete } from '../utils/confirm';

interface AgentListProps {
  currentSessionId: number | null;
  onSelectSession: (session: Session | null) => void;
  onCreateSession: (agentId: number) => void;
}

const AgentList: React.FC<AgentListProps> = ({ currentSessionId, onSelectSession, onCreateSession }) => {
  const { t } = useTranslation();
  const [agents, setAgents] = useState<AgentWithSessions[]>([]);
  const [loading, setLoading] = useState(false);
  const [activeKeys, setActiveKeys] = useState<string[]>([]);
  const [hoveredAgentId, setHoveredAgentId] = useState<number | null>(null);
  const [hoveredSessionId, setHoveredSessionId] = useState<number | null>(null);
  const [editModalVisible, setEditModalVisible] = useState(false);
  const [editingSession, setEditingSession] = useState<SessionBrief | null>(null);
  const [editTitle, setEditTitle] = useState('');

  const loadAgents = async () => {
    setLoading(true);
    try {
      const response = await agentApi.listWithSessions();
      setAgents(response.data);
      if (response.data.length > 0 && activeKeys.length === 0) {
        setActiveKeys([`agent-${response.data[0].id}`]);
      }
    } catch (error) {
      logger.error('Failed to load agents:', error);
      message.error(t('messages.loadFailed'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadAgents();
  }, []);

  const handleDeleteSession = async (sessionId: number, e: React.MouseEvent) => {
    e.stopPropagation();
    
    confirmDelete({
      title: t('session.confirmDeleteTitle'),
      content: t('session.confirmDelete'),
      okText: t('common.delete'),
      cancelText: t('common.cancel'),
      onOk: async () => {
        try {
          await sessionApi.delete(sessionId);
          await loadAgents();
          message.success(t('session.deleteSuccess'));
          if (currentSessionId === sessionId) {
            onSelectSession(null);
          }
        } catch (error) {
          logger.error('Failed to delete session:', error);
          message.error(t('session.deleteFailed'));
        }
      },
    });
  };

  const handleEditSession = (session: SessionBrief, e: React.MouseEvent) => {
    e.stopPropagation();
    setEditingSession(session);
    setEditTitle(session.title || '');
    setEditModalVisible(true);
  };

  const handleUpdateTitle = async () => {
    if (!editingSession || !editTitle.trim()) {
      message.error(t('session.titleEmpty'));
      return;
    }

    try {
      await sessionApi.update(editingSession.id, { title: editTitle.trim() });
      await loadAgents();
      message.success(t('session.updateSuccess'));
      setEditModalVisible(false);
      setEditingSession(null);
      setEditTitle('');
    } catch (error) {
      logger.error('Failed to update session title:', error);
      message.error(t('session.updateFailed'));
    }
  };

  const handleSelectSession = (session: SessionBrief) => {
    onSelectSession({
      ...session,
      agent_id: agents.find(a => a.sessions.some(s => s.id === session.id))?.id || 0
    });
  };

  return (
    <div style={{ flex: 1, overflowY: 'auto', padding: '8px 0' }}>
      {loading ? (
        <div style={{ textAlign: 'center', padding: '20px', color: '#9ca3af' }}>
          {t('sidebar.loading')}
        </div>
      ) : agents.length === 0 ? (
        <div style={{ textAlign: 'center', padding: '20px', color: '#9ca3af' }}>
          {t('sidebar.noAgent')}
        </div>
      ) : (
        <Collapse
          activeKey={activeKeys}
          onChange={(keys) => setActiveKeys(keys as string[])}
          ghost
          expandIconPlacement="end"
          styles={{
            header: { padding: '10px 12px' },
            body: { padding: '0 12px 4px 12px' }
          }}
          items={agents.map(agent => ({
            key: `agent-${agent.id}`,
            label: (
              <div 
                style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}
                onMouseEnter={() => setHoveredAgentId(agent.id)}
                onMouseLeave={() => setHoveredAgentId(null)}
              >
                <div style={{ display: 'flex', alignItems: 'center', flex: 1, minWidth: 0 }}>
                  <RobotOutlined style={{ marginRight: '8px', fontSize: '14px' }} />
                  <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {agent.name}
                  </span>
                </div>
                {hoveredAgentId === agent.id && (
                  <Button
                    type="text"
                    size="small"
                    icon={<MessageCircle size={16} />}
                    onClick={(e) => {
                      e.stopPropagation();
                      onCreateSession(agent.id);
                    }}
                    style={{ 
                      color: '#6b7280',
                      padding: '4px 8px',
                      height: 'auto'
                    }}
                  />
                )}
              </div>
            ),
            children: (
              <div>
                {agent.sessions.length === 0 ? (
                  <div style={{ textAlign: 'center', padding: '8px', color: '#9ca3af', fontSize: '12px' }}>
                    {t('sidebar.noSession')}
                  </div>
                ) : (
                  agent.sessions.map((session) => (
                    <div
                      key={session.id}
                      className={`session-item ${currentSessionId === session.id ? 'active' : ''}`}
                      onClick={() => handleSelectSession(session)}
                      onMouseEnter={() => setHoveredSessionId(session.id)}
                      onMouseLeave={() => setHoveredSessionId(null)}
                    >
                      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                        <div style={{ display: 'flex', alignItems: 'center', flex: 1, minWidth: 0 }}>
                          <MessageOutlined style={{ marginRight: '6px', fontSize: '12px', color: '#9ca3af' }} />
                          <span style={{ 
                            overflow: 'hidden', 
                            textOverflow: 'ellipsis', 
                            whiteSpace: 'nowrap',
                            fontSize: '13px'
                          }}>
                            {session.title || t('session.untitled')}
                          </span>
                        </div>
                        {hoveredSessionId === session.id && (
                          <div style={{ display: 'flex', gap: '2px' }}>
                            <Button
                              type="text"
                              size="small"
                              icon={<EditOutlined />}
                              onClick={(e) => handleEditSession(session, e)}
                              style={{ color: '#6b7280', padding: '2px 4px', height: 'auto', minWidth: 'auto' }}
                            />
                            <Button
                              type="text"
                              size="small"
                              danger
                              icon={<DeleteOutlined />}
                              onClick={(e) => handleDeleteSession(session.id, e)}
                              style={{ padding: '2px 4px', height: 'auto', minWidth: 'auto' }}
                            />
                          </div>
                        )}
                      </div>
                    </div>
                  ))
                )}
              </div>
            ),
          }))}
        />
      )}

      <Modal
        title={t('session.editTitle')}
        open={editModalVisible}
        onOk={handleUpdateTitle}
        onCancel={() => {
          setEditModalVisible(false);
          setEditingSession(null);
          setEditTitle('');
        }}
        okText={t('common.save')}
        cancelText={t('common.cancel')}
      >
        <Input
          value={editTitle}
          onChange={(e) => setEditTitle(e.target.value)}
          placeholder={t('session.titlePlaceholder')}
          onPressEnter={handleUpdateTitle}
          style={{ marginTop: '16px' }}
        />
      </Modal>
    </div>
  );
};

export default AgentList;
import React, { useEffect, useState, useMemo } from 'react';
import { Button, Modal, message, Input, Dropdown } from 'antd';
import { DeleteOutlined, EditOutlined } from '@ant-design/icons';
import { MessageCircle } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { MenuProps } from 'antd';
import type { AgentWithSessions, Session, SessionWithAgent } from '../types';
import { agentApi, sessionApi } from '../services/api';
import { logger } from '../logger';
import { confirmDelete } from '../utils/confirm';
import AgentAvatar from './AgentAvatar';

interface AgentListProps {
  currentSessionId: number | null;
  onSelectSession: (session: Session | null) => void;
  onCreateSession: (agentId: number) => void;
}

const AgentList: React.FC<AgentListProps> = ({ currentSessionId, onSelectSession, onCreateSession }) => {
  const { t } = useTranslation();
  const [agents, setAgents] = useState<AgentWithSessions[]>([]);
  const [loading, setLoading] = useState(false);
  const [hoveredSessionId, setHoveredSessionId] = useState<number | null>(null);
  const [editModalVisible, setEditModalVisible] = useState(false);
  const [editingSession, setEditingSession] = useState<SessionWithAgent | null>(null);
  const [editTitle, setEditTitle] = useState('');

  // Flatten agents with sessions into a single session list with agent info
  const flatSessions: SessionWithAgent[] = useMemo(() => {
    const sessions: SessionWithAgent[] = [];
    agents.forEach(agent => {
      agent.sessions.forEach(session => {
        sessions.push({
          ...session,
          agent_id: agent.id,
          agent_name: agent.name,
          agent_avatar: agent.avatar,
        });
      });
    });
    // Sort by updated_at descending (most recent first)
    sessions.sort((a, b) => {
      const timeA = new Date(a.updated_at || a.created_at).getTime();
      const timeB = new Date(b.updated_at || b.created_at).getTime();
      return timeB - timeA;
    });
    return sessions;
  }, [agents]);

  const loadAgents = async () => {
    setLoading(true);
    try {
      const response = await agentApi.listWithSessions();
      setAgents(response.data);
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

  const handleEditSession = (session: SessionWithAgent, e: React.MouseEvent) => {
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

  const handleSelectSession = (session: SessionWithAgent) => {
    onSelectSession({
      id: session.id,
      title: session.title,
      agent_id: session.agent_id,
      status: session.status,
      created_at: session.created_at,
      updated_at: session.updated_at,
    });
  };

  // Dropdown menu for creating new session with agent selection
  const agentMenuItems: MenuProps['items'] = agents.map(agent => ({
    key: `agent-${agent.id}`,
    label: (
      <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
        <AgentAvatar avatar={agent.avatar} size={24} iconSize={12} borderRadius="6px" />
        <span>{agent.name}</span>
      </div>
    ),
    onClick: () => onCreateSession(agent.id),
  }));

  return (
    <div className="app-sidebar">
      {/* Header toolbar */}
      <div className="sidebar-header">
        <Dropdown
          menu={{ items: agentMenuItems }}
          trigger={['click']}
          disabled={agents.length === 0}
          placement="bottomLeft"
        >
          <Button
            type="text"
            icon={<MessageCircle size={18} />}
            disabled={agents.length === 0}
            title={t('sidebar.newChat')}
            style={{ color: 'var(--color-text-secondary)' }}
          />
        </Dropdown>
      </div>

      {/* Session list */}
      <div className="sidebar-content">
        {loading ? (
          <div style={{ textAlign: 'center', padding: '20px', color: 'var(--color-text-placeholder)' }}>
            {t('sidebar.loading')}
          </div>
        ) : flatSessions.length === 0 ? (
          <div style={{ textAlign: 'center', padding: '20px', color: 'var(--color-text-placeholder)' }}>
            {t('sidebar.noSession')}
          </div>
        ) : (
          flatSessions.map((session) => (
            <div
              key={session.id}
              className={`session-item ${currentSessionId === session.id ? 'active' : ''}`}
              onClick={() => handleSelectSession(session)}
              onMouseEnter={() => setHoveredSessionId(session.id)}
              onMouseLeave={() => setHoveredSessionId(null)}
            >
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                <div style={{ display: 'flex', alignItems: 'center', flex: 1, minWidth: 0 }}>
                  <AgentAvatar
                    avatar={session.agent_avatar}
                    size={24}
                    iconSize={12}
                    borderRadius="6px"
                  />
                  <span style={{
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                    fontSize: '13px',
                    marginLeft: '10px'
                  }}>
                    {session.title || t('session.untitled')}
                  </span>
                </div>
                {hoveredSessionId === session.id && (
                  <div style={{ display: 'flex', alignItems: 'center', gap: '2px' }}>
                    <Button
                      type="text"
                      size="small"
                      icon={<EditOutlined />}
                      onClick={(e) => handleEditSession(session, e)}
                      style={{ color: 'var(--color-text-secondary)', padding: '2px 4px', height: 'auto', minWidth: 'auto' }}
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

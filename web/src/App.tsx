import { useState } from 'react';
import { Button, Modal, Dropdown } from 'antd';
import type { MenuProps } from 'antd';
import { SettingOutlined, RobotOutlined, UserOutlined, PlusOutlined, CloseOutlined, GlobalOutlined, CheckOutlined, ApiOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import { changeLanguage, getCurrentLanguage } from './i18n';
import AgentList from './components/AgentList';
import ChatWindow from './components/ChatWindow';
import LLMConfigList from './components/LLMConfigList';
import EmbeddingConfigList from './components/EmbeddingConfigList';
import AgentConfig from './components/AgentConfig';
import type { Session, LLMConfig, EmbeddingConfig } from './types';
import './App.css';

function App() {
  const { t } = useTranslation();
  const [currentSession, setCurrentSession] = useState<Session | null>(null);
  const [showLLMConfig, setShowLLMConfig] = useState(false);
  const [showEmbeddingConfig, setShowEmbeddingConfig] = useState(false);
  const [showAgentConfig, setShowAgentConfig] = useState(false);
  const [refreshKey, setRefreshKey] = useState(0);
  const [showCreateAgent, setShowCreateAgent] = useState(false);
  const [showCreateLLM, setShowCreateLLM] = useState(false);
  const [showCreateEmbedding, setShowCreateEmbedding] = useState(false);
  const [currentLang, setCurrentLang] = useState(getCurrentLanguage());

  const handleSelectSession = (session: Session | null) => {
    setCurrentSession(session);
  };

  const handleSelectLLMConfig = (config: LLMConfig | null) => {
    if (currentSession && config) {
      setCurrentSession(prev => prev ? {
        ...prev,
        llm_config_id: config.id
      } : null);
    }
  };

  const handleSelectEmbeddingConfig = (config: EmbeddingConfig | null) => {
    console.log('Selected embedding config:', config);
  };

  const handleCreateSession = (agentId: number) => {
    const tempSession: Session = {
      id: -1,
      title: 'New Chat',
      agent_id: agentId,
      status: 1,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };
    setCurrentSession(tempSession);
  };

  const handleLanguageChange = (lang: string) => {
    changeLanguage(lang);
    setCurrentLang(lang);
  };

  const handleAgentCreated = () => {
    setRefreshKey(prev => prev + 1);
  };

  const languageMenuItems: MenuProps['items'] = [
    {
      key: 'zh',
      label: (
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', minWidth: '100px' }}>
          <span>中文</span>
          {currentLang === 'zh' && <CheckOutlined style={{ color: '#1890ff' }} />}
        </div>
      ),
      onClick: () => handleLanguageChange('zh')
    },
    {
      key: 'en',
      label: (
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', minWidth: '100px' }}>
          <span>English</span>
          {currentLang === 'en' && <CheckOutlined style={{ color: '#1890ff' }} />}
        </div>
      ),
      onClick: () => handleLanguageChange('en')
    }
  ];

  const settingsMenuItems: MenuProps['items'] = [
    {
      key: 'agent',
      label: t('settings.agentConfig'),
      icon: <UserOutlined />,
      onClick: () => setShowAgentConfig(true)
    },
    {
      key: 'llm',
      label: t('settings.llmConfig'),
      icon: <RobotOutlined />,
      onClick: () => setShowLLMConfig(true)
    },
    {
      key: 'embedding',
      label: t('settings.embeddingConfig'),
      icon: <ApiOutlined />,
      onClick: () => setShowEmbeddingConfig(true)
    },
    {
      key: 'language',
      label: t('settings.language'),
      icon: <GlobalOutlined />,
      children: languageMenuItems
    }
  ];

  return (
    <div className="app-container">
      <header className="app-header">
        <div className="app-logo">Private Buddy</div>
      </header>

      <div className="app-body">
        <aside className="app-sidebar">
          <AgentList
            key={refreshKey}
            currentSessionId={currentSession?.id || null}
            onSelectSession={handleSelectSession}
            onCreateSession={handleCreateSession}
          />
          <div style={{ padding: '12px', borderTop: '1px solid #f3f4f6' }}>
            <Dropdown
              menu={{ items: settingsMenuItems }}
              trigger={['click']}
              placement="topLeft"
            >
              <Button
                type="text"
                icon={<SettingOutlined />}
                block
                style={{
                  borderRadius: '8px',
                  height: '40px',
                  justifyContent: 'flex-start',
                  color: '#6b7280'
                }}
              >
                {t('settings.title')}
              </Button>
            </Dropdown>
          </div>
        </aside>

        <main className="app-main">
          <div className="chat-container">
            <ChatWindow 
              session={currentSession} 
              onSessionCreated={(sessionId) => {
                setRefreshKey(prev => prev + 1);
                setCurrentSession(prev => prev ? { ...prev, id: sessionId } : null);
              }}
            />
          </div>
        </main>
      </div>

      <Modal
        title={
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <Button
              type="text"
              icon={<CloseOutlined />}
              onClick={() => setShowLLMConfig(false)}
              style={{ color: '#6b7280' }}
            />
            <span>{t('llmConfig.title')}</span>
            <Button
              type="text"
              icon={<PlusOutlined />}
              onClick={() => setShowCreateLLM(true)}
              style={{ color: '#6b7280' }}
            />
          </div>
        }
        open={showLLMConfig}
        onCancel={() => setShowLLMConfig(false)}
        footer={null}
        width={600}
        closable={false}
      >
        <LLMConfigList 
          onSelectConfig={handleSelectLLMConfig}
          showCreate={showCreateLLM}
          onCreateClose={() => setShowCreateLLM(false)}
        />
      </Modal>

      <Modal
        title={
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <Button
              type="text"
              icon={<CloseOutlined />}
              onClick={() => setShowAgentConfig(false)}
              style={{ color: '#6b7280' }}
            />
            <span>{t('agent.title')}</span>
            <Button
              type="text"
              icon={<PlusOutlined />}
              onClick={() => setShowCreateAgent(true)}
              style={{ color: '#6b7280' }}
            />
          </div>
        }
        open={showAgentConfig}
        onCancel={() => setShowAgentConfig(false)}
        footer={null}
        width={700}
        closable={false}
      >
        <AgentConfig 
          showCreate={showCreateAgent}
          onCreateClose={() => setShowCreateAgent(false)}
          onAgentCreated={handleAgentCreated}
        />
      </Modal>

      <Modal
        title={
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <Button
              type="text"
              icon={<CloseOutlined />}
              onClick={() => setShowEmbeddingConfig(false)}
              style={{ color: '#6b7280' }}
            />
            <span>{t('embeddingConfig.title')}</span>
            <Button
              type="text"
              icon={<PlusOutlined />}
              onClick={() => setShowCreateEmbedding(true)}
              style={{ color: '#6b7280' }}
            />
          </div>
        }
        open={showEmbeddingConfig}
        onCancel={() => setShowEmbeddingConfig(false)}
        footer={null}
        width={600}
        closable={false}
      >
        <EmbeddingConfigList 
          onSelectConfig={handleSelectEmbeddingConfig}
          showCreate={showCreateEmbedding}
          onCreateClose={() => setShowCreateEmbedding(false)}
        />
      </Modal>
    </div>
  );
}

export default App;
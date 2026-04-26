import { useState } from 'react';
import { Button, Dropdown } from 'antd';
import type { MenuProps } from 'antd';
import { SettingOutlined, RobotOutlined, UserOutlined, PlusOutlined, GlobalOutlined, CheckOutlined, ApiOutlined, ArrowLeftOutlined, SearchOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import { changeLanguage, getCurrentLanguage } from './i18n';
import AgentList from './components/AgentList';
import ChatWindow from './components/ChatWindow';
import LLMConfigList from './components/LLMConfigList';
import EmbeddingConfigList from './components/EmbeddingConfigList';
import AgentConfig from './components/AgentConfig';
import SearchConfigForm from './components/SearchConfigForm';
import type { Session, LLMConfig, EmbeddingConfig } from './types';
import './App.css';

type MainView = 'chat' | 'agent' | 'llm' | 'embedding' | 'search';

function App() {
  const { t } = useTranslation();
  const [currentSession, setCurrentSession] = useState<Session | null>(null);
  const [mainView, setMainView] = useState<MainView>('chat');
  const [refreshKey, setRefreshKey] = useState(0);
  const [showCreateAgent, setShowCreateAgent] = useState(false);
  const [showCreateLLM, setShowCreateLLM] = useState(false);
  const [showCreateEmbedding, setShowCreateEmbedding] = useState(false);
  const [currentLang, setCurrentLang] = useState(getCurrentLanguage());

  const handleSelectSession = (session: Session | null) => {
    setCurrentSession(session);
    setMainView('chat');
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
    setMainView('chat');
  };

  const handleLanguageChange = (lang: string) => {
    changeLanguage(lang);
    setCurrentLang(lang);
  };

  const handleAgentCreated = () => {
    setRefreshKey(prev => prev + 1);
  };

  const switchToChat = () => {
    setMainView('chat');
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
      onClick: () => setMainView('agent')
    },
    {
      key: 'llm',
      label: t('settings.llmConfig'),
      icon: <RobotOutlined />,
      onClick: () => setMainView('llm')
    },
    {
      key: 'embedding',
      label: t('settings.embeddingConfig'),
      icon: <ApiOutlined />,
      onClick: () => setMainView('embedding')
    },
    {
      key: 'search',
      label: t('settings.searchConfig'),
      icon: <SearchOutlined />,
      onClick: () => setMainView('search')
    },
    {
      key: 'language',
      label: t('settings.language'),
      icon: <GlobalOutlined />,
      children: languageMenuItems
    }
  ];

  const renderMainContent = () => {
    switch (mainView) {
      case 'agent':
        return (
          <div className="main-panel">
            <div className="main-panel-header">
              <Button
                type="text"
                icon={<ArrowLeftOutlined />}
                onClick={switchToChat}
                style={{ color: '#6b7280' }}
              />
              <span className="main-panel-title">{t('agent.title')}</span>
              <Button
                type="text"
                icon={<PlusOutlined />}
                onClick={() => setShowCreateAgent(true)}
                style={{ color: '#6b7280' }}
              />
            </div>
            <div className="main-panel-body">
              <AgentConfig
                showCreate={showCreateAgent}
                onCreateClose={() => setShowCreateAgent(false)}
                onAgentCreated={handleAgentCreated}
              />
            </div>
          </div>
        );

      case 'llm':
        return (
          <div className="main-panel">
            <div className="main-panel-header">
              <Button
                type="text"
                icon={<ArrowLeftOutlined />}
                onClick={switchToChat}
                style={{ color: '#6b7280' }}
              />
              <span className="main-panel-title">{t('llmConfig.title')}</span>
              <Button
                type="text"
                icon={<PlusOutlined />}
                onClick={() => setShowCreateLLM(true)}
                style={{ color: '#6b7280' }}
              />
            </div>
            <div className="main-panel-body">
              <LLMConfigList
                onSelectConfig={handleSelectLLMConfig}
                showCreate={showCreateLLM}
                onCreateClose={() => setShowCreateLLM(false)}
              />
            </div>
          </div>
        );

      case 'embedding':
        return (
          <div className="main-panel">
            <div className="main-panel-header">
              <Button
                type="text"
                icon={<ArrowLeftOutlined />}
                onClick={switchToChat}
                style={{ color: '#6b7280' }}
              />
              <span className="main-panel-title">{t('embeddingConfig.title')}</span>
              <Button
                type="text"
                icon={<PlusOutlined />}
                onClick={() => setShowCreateEmbedding(true)}
                style={{ color: '#6b7280' }}
              />
            </div>
            <div className="main-panel-body">
              <EmbeddingConfigList
                onSelectConfig={handleSelectEmbeddingConfig}
                showCreate={showCreateEmbedding}
                onCreateClose={() => setShowCreateEmbedding(false)}
              />
            </div>
          </div>
        );

      case 'search':
        return (
          <div className="main-panel">
            <div className="main-panel-header">
              <Button
                type="text"
                icon={<ArrowLeftOutlined />}
                onClick={switchToChat}
                style={{ color: '#6b7280' }}
              />
              <span className="main-panel-title">{t('searchConfig.title')}</span>
            </div>
            <div className="main-panel-body">
              <SearchConfigForm />
            </div>
          </div>
        );

      case 'chat':
      default:
        return (
          <div className="chat-container">
            <ChatWindow
              session={currentSession}
              onSessionCreated={(sessionId) => {
                setRefreshKey(prev => prev + 1);
                setCurrentSession(prev => prev ? { ...prev, id: sessionId } : null);
              }}
            />
          </div>
        );
    }
  };

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
                  color: ['agent', 'llm', 'embedding'].includes(mainView) ? '#1890ff' : '#6b7280',
                  backgroundColor: ['agent', 'llm', 'embedding'].includes(mainView) ? '#e6f7ff' : 'transparent',
                }}
              >
                {t('settings.title')}
              </Button>
            </Dropdown>
          </div>
        </aside>

        <main className="app-main">
          {renderMainContent()}
        </main>
      </div>
    </div>
  );
}

export default App;

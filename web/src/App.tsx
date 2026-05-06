import { useState, useEffect } from 'react';
import { Button, Tooltip, message } from 'antd';
import { SettingOutlined, PlusOutlined, ArrowLeftOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import { changeLanguage, getCurrentLanguage } from './i18n';
import AgentList from './components/AgentList';
import ChatWindow from './components/ChatWindow';
import LLMConfigList from './components/LLMConfigList';
import EmbeddingConfigList from './components/EmbeddingConfigList';
import AgentConfig from './components/AgentConfig';
import SearchConfigForm from './components/SearchConfigForm';
import { ConfigIcon } from './components/AgentAvatar';
import { versionApi, initApiClient } from './services/api';
import type { IconType } from './components/AgentAvatar';
import type { Session, LLMConfig, EmbeddingConfig } from './types';
import './App.css';

type RightPanelView = null | 'settings-overview' | 'settings-agent' | 'settings-llm' | 'settings-embedding' | 'settings-search' | 'settings-language';

const SETTINGS_CARDS: { key: string; iconType: IconType }[] = [
  { key: 'settings-agent', iconType: 'agent' },
  { key: 'settings-llm', iconType: 'llm' },
  { key: 'settings-embedding', iconType: 'embedding' },
  { key: 'settings-search', iconType: 'search' },
  { key: 'settings-language', iconType: 'language' },
];

function App() {
  const { t } = useTranslation();
  const [currentSession, setCurrentSession] = useState<Session | null>(null);
  const [rightPanelView, setRightPanelView] = useState<RightPanelView>(null);
  const [refreshKey, setRefreshKey] = useState(0);
  const [showCreateAgent, setShowCreateAgent] = useState(false);
  const [showCreateLLM, setShowCreateLLM] = useState(false);
  const [showCreateEmbedding, setShowCreateEmbedding] = useState(false);
  const [currentLang, setCurrentLang] = useState(getCurrentLanguage());
  const [version, setVersion] = useState<string>('');
  const [isMacElectron, setIsMacElectron] = useState(false);
  const [isWinLinuxElectron, setIsWinLinuxElectron] = useState(false);

  useEffect(() => {
    if (window.electronAPI) {
      window.electronAPI.getPlatform().then(platform => {
        setIsMacElectron(platform === 'darwin');
        setIsWinLinuxElectron(platform !== 'darwin');
      });
      initApiClient();
    }
  }, []);

  useEffect(() => {
    if (!window.electronAPI?.onBackendStatus) return;
    const unsubscribe = window.electronAPI.onBackendStatus((status) => {
      if (status === 'ready') {
        setRefreshKey(prev => prev + 1);
        versionApi.get()
          .then(res => setVersion(res.data.version))
          .catch(() => setVersion(''));
      }
    });
    return unsubscribe;
  }, []);

  useEffect(() => {
    if (!window.electronAPI?.onBackendError) return;
    const unsubscribe = window.electronAPI.onBackendError((error) => {
      message.error(`Backend failed to start: ${error}`, 0);
    });
    return unsubscribe;
  }, []);

  useEffect(() => {
    versionApi.get()
      .then(res => setVersion(res.data.version))
      .catch(() => setVersion(''));
  }, []);

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

  const settingsLabelMap: Record<string, string> = {
    'settings-agent': t('settings.agentConfig'),
    'settings-llm': t('settings.llmConfig'),
    'settings-embedding': t('settings.embeddingConfig'),
    'settings-search': t('settings.searchConfig'),
    'settings-language': t('settings.language'),
  };

  const isSettingsOpen = rightPanelView !== null;

  const renderSettingsOverview = () => (
    <div className="panel-overview">
      <div className="panel-overview-title">{t('settings.title')}</div>
      <div className="panel-card-grid">
        {SETTINGS_CARDS.map(({ key, iconType }) => (
          <div
            key={key}
            className="panel-card"
            onClick={() => setRightPanelView(key as RightPanelView)}
          >
            <ConfigIcon type={iconType} size={48} iconSize={22} borderRadius="12px" marginBottom={12} />
            <div className="panel-card-label">{settingsLabelMap[key]}</div>
          </div>
        ))}
      </div>
    </div>
  );

  const renderLanguagePanel = () => (
    <div className="panel-detail">
      <div className="panel-detail-header">
        <Button
          type="text"
          icon={<ArrowLeftOutlined />}
          onClick={() => setRightPanelView('settings-overview')}
          style={{ color: 'var(--color-text-secondary)' }}
        />
        <span className="panel-detail-title">{t('settings.language')}</span>
      </div>
      <div className="panel-detail-body">
        <div className="lang-options">
          <div
            className={`lang-option-card ${currentLang === 'zh' ? 'active' : ''}`}
            onClick={() => handleLanguageChange('zh')}
          >
            <span className="lang-option-text">中文</span>
          </div>
          <div
            className={`lang-option-card ${currentLang === 'en' ? 'active' : ''}`}
            onClick={() => handleLanguageChange('en')}
          >
            <span className="lang-option-text">English</span>
          </div>
        </div>
      </div>
    </div>
  );

  const renderRightPanel = () => {
    switch (rightPanelView) {
      case 'settings-overview':
        return renderSettingsOverview();

      case 'settings-agent':
        return (
          <div className="panel-detail">
            <div className="panel-detail-header">
              <Button
                type="text"
                icon={<ArrowLeftOutlined />}
                onClick={() => setRightPanelView('settings-overview')}
                style={{ color: 'var(--color-text-secondary)' }}
              />
              <span className="panel-detail-title">{t('agent.title')}</span>
              <Button
                type="text"
                icon={<PlusOutlined />}
                onClick={() => setShowCreateAgent(true)}
                style={{ color: 'var(--color-text-secondary)' }}
              />
            </div>
            <div className="panel-detail-body">
              <AgentConfig
                showCreate={showCreateAgent}
                onCreateClose={() => setShowCreateAgent(false)}
                onAgentCreated={handleAgentCreated}
              />
            </div>
          </div>
        );

      case 'settings-llm':
        return (
          <div className="panel-detail">
            <div className="panel-detail-header">
              <Button
                type="text"
                icon={<ArrowLeftOutlined />}
                onClick={() => setRightPanelView('settings-overview')}
                style={{ color: 'var(--color-text-secondary)' }}
              />
              <span className="panel-detail-title">{t('llmConfig.title')}</span>
              <Button
                type="text"
                icon={<PlusOutlined />}
                onClick={() => setShowCreateLLM(true)}
                style={{ color: 'var(--color-text-secondary)' }}
              />
            </div>
            <div className="panel-detail-body">
              <LLMConfigList
                onSelectConfig={handleSelectLLMConfig}
                showCreate={showCreateLLM}
                onCreateClose={() => setShowCreateLLM(false)}
              />
            </div>
          </div>
        );

      case 'settings-embedding':
        return (
          <div className="panel-detail">
            <div className="panel-detail-header">
              <Button
                type="text"
                icon={<ArrowLeftOutlined />}
                onClick={() => setRightPanelView('settings-overview')}
                style={{ color: 'var(--color-text-secondary)' }}
              />
              <span className="panel-detail-title">{t('embeddingConfig.title')}</span>
              <Button
                type="text"
                icon={<PlusOutlined />}
                onClick={() => setShowCreateEmbedding(true)}
                style={{ color: 'var(--color-text-secondary)' }}
              />
            </div>
            <div className="panel-detail-body">
              <EmbeddingConfigList
                onSelectConfig={handleSelectEmbeddingConfig}
                showCreate={showCreateEmbedding}
                onCreateClose={() => setShowCreateEmbedding(false)}
              />
            </div>
          </div>
        );

      case 'settings-search':
        return (
          <div className="panel-detail">
            <div className="panel-detail-header">
              <Button
                type="text"
                icon={<ArrowLeftOutlined />}
                onClick={() => setRightPanelView('settings-overview')}
                style={{ color: 'var(--color-text-secondary)' }}
              />
              <span className="panel-detail-title">{t('searchConfig.title')}</span>
            </div>
            <div className="panel-detail-body">
              <SearchConfigForm />
            </div>
          </div>
        );

      case 'settings-language':
        return renderLanguagePanel();

      default:
        return null;
    }
  };

  return (
    <div className="app-container">
      <header className={`app-header${isMacElectron ? ' app-header-mac' : ''}${isWinLinuxElectron ? ' app-header-win-linux' : ''}`}>
        <Tooltip title={version ? `v${version}` : ''} placement="right">
          <div className="app-logo">
            <img src="./favicon.svg" alt="logo" className="app-logo-img" />
            Private Buddy
          </div>
        </Tooltip>
        <div className="app-header-actions">
          <Button
            type="text"
            icon={<SettingOutlined />}
            onClick={() => setRightPanelView(isSettingsOpen ? null : 'settings-overview')}
            style={{
              color: isSettingsOpen ? 'var(--color-active)' : 'var(--color-text-secondary)',
              fontSize: '18px',
            }}
          />
        </div>
      </header>

      <div className="app-body">
        <aside className="app-sidebar">
          <AgentList
            key={refreshKey}
            currentSessionId={currentSession?.id || null}
            onSelectSession={handleSelectSession}
            onCreateSession={handleCreateSession}
          />
        </aside>

        <div className="app-content">
          <div className="app-chat-area">
            <ChatWindow
              session={currentSession}
              onSessionCreated={(sessionId) => {
                setRefreshKey(prev => prev + 1);
                setCurrentSession(prev => prev ? { ...prev, id: sessionId } : null);
              }}
            />
          </div>

          {rightPanelView && (
            <div className="app-right-panel">
              {renderRightPanel()}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

export default App;

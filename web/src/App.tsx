import { useState, useEffect } from 'react';
import { Button, Tooltip, message } from 'antd';
import { SettingOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import { changeLanguage, getCurrentLanguage } from './i18n';
import useScrolling from './hooks/useScrolling';
import AgentList from './components/AgentList';
import ChatWindow from './components/ChatWindow';
import LLMConfigList from './components/LLMConfigList';
import EmbeddingConfigList from './components/EmbeddingConfigList';
import AgentConfig from './components/AgentConfig';
import SearchConfigForm from './components/SearchConfigForm';
import ResizableCard from './components/ResizableCard';
import PanelDetail from './components/PanelDetail';
import KnowledgeBaseList from './components/KnowledgeBaseList';
import KnowledgeBaseDetail from './components/KnowledgeBaseDetail';
import { ConfigIcon } from './components/AgentAvatar';
import { versionApi, initApiClient } from './services/api';
import { logger } from './logger';
import type { IconType } from './components/AgentAvatar';
import type { Session, LLMConfig, KnowledgeBase } from './types';
import './App.css';

type RightPanelView = null | 'settings-overview' | 'settings-agent' | 'settings-llm' | 'settings-embedding' | 'settings-search' | 'settings-language' | 'settings-kb' | 'settings-kb-detail';

const SETTINGS_CARDS: { key: string; iconType: IconType }[] = [
  { key: 'settings-agent', iconType: 'agent' },
  { key: 'settings-kb', iconType: 'kb' },
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
  const [showCreateKB, setShowCreateKB] = useState(false);
  const [selectedKB, setSelectedKB] = useState<KnowledgeBase | null>(null);
  const [currentLang, setCurrentLang] = useState(getCurrentLanguage());
  const [version, setVersion] = useState<string>('');
  const [isMacElectron, setIsMacElectron] = useState(false);
  const [isWinLinuxElectron, setIsWinLinuxElectron] = useState(false);

  useScrolling();

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

  const handleCreateSession = (agentId: number) => {
    logger.info('handleCreateSession called with agentId:', agentId);
    const tempSession: Session = {
      id: -1,
      title: 'New Chat',
      agent_id: agentId,
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

  const goBackToSettings = () => setRightPanelView('settings-overview');

  const settingsLabelMap: Record<string, string> = {
    'settings-agent': t('settings.agentConfig'),
    'settings-kb': t('settings.kbConfig'),
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
    <PanelDetail title={t('settings.language')} onBack={goBackToSettings}>
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
    </PanelDetail>
  );

  const renderRightPanel = () => {
    switch (rightPanelView) {
      case 'settings-overview':
        return renderSettingsOverview();

      case 'settings-agent':
        return (
          <PanelDetail
            title={t('agent.title')}
            onBack={goBackToSettings}
            onAdd={() => setShowCreateAgent(true)}
          >
            <AgentConfig
              showCreate={showCreateAgent}
              onCreateClose={() => setShowCreateAgent(false)}
              onAgentCreated={handleAgentCreated}
            />
          </PanelDetail>
        );

      case 'settings-kb':
        return (
          <PanelDetail
            title={t('kb.title')}
            onBack={goBackToSettings}
            onAdd={() => setShowCreateKB(true)}
          >
            <KnowledgeBaseList
              showCreate={showCreateKB}
              onCreateClose={() => setShowCreateKB(false)}
              onSelectKB={(kb) => {
                setSelectedKB(kb);
                setRightPanelView('settings-kb-detail');
              }}
            />
          </PanelDetail>
        );

      case 'settings-kb-detail':
        return selectedKB ? (
          <PanelDetail
            title={selectedKB.name}
            onBack={() => {
              setSelectedKB(null);
              setShowCreateKB(false);
              setRightPanelView('settings-kb');
            }}
          >
            <KnowledgeBaseDetail
              kb={selectedKB}
              onBack={() => {
                setSelectedKB(null);
                setShowCreateKB(false);
                setRightPanelView('settings-kb');
              }}
            />
          </PanelDetail>
        ) : null;

      case 'settings-llm':
        return (
          <PanelDetail
            title={t('llmConfig.title')}
            onBack={goBackToSettings}
            onAdd={() => setShowCreateLLM(true)}
          >
            <LLMConfigList
              onSelectConfig={handleSelectLLMConfig}
              showCreate={showCreateLLM}
              onCreateClose={() => setShowCreateLLM(false)}
            />
          </PanelDetail>
        );

      case 'settings-embedding':
        return (
          <PanelDetail
            title={t('embeddingConfig.title')}
            onBack={goBackToSettings}
            onAdd={() => setShowCreateEmbedding(true)}
          >
            <EmbeddingConfigList
              showCreate={showCreateEmbedding}
              onCreateClose={() => setShowCreateEmbedding(false)}
            />
          </PanelDetail>
        );

      case 'settings-search':
        return (
          <PanelDetail title={t('searchConfig.title')} onBack={goBackToSettings}>
            <SearchConfigForm />
          </PanelDetail>
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
        <ResizableCard
          defaultWidth={280}
          minWidth={200}
          maxWidth={400}
          resizeSide="right"
          className="app-sidebar-wrapper"
        >
          <AgentList
            key={refreshKey}
            currentSessionId={currentSession?.id || null}
            onSelectSession={handleSelectSession}
            onCreateSession={handleCreateSession}
          />
        </ResizableCard>

        <div className="app-content">
          <ResizableCard flex className="app-chat-area-wrapper">
            <ChatWindow
              session={currentSession}
              onSessionCreated={(sessionId) => {
                setRefreshKey(prev => prev + 1);
                setCurrentSession(prev => prev ? { ...prev, id: sessionId } : null);
              }}
            />
          </ResizableCard>

          {rightPanelView && (
            <ResizableCard
              defaultWidth={480}
              minWidth={320}
              maxWidth={800}
              resizeSide="left"
              className="app-right-panel-wrapper"
            >
              {renderRightPanel()}
            </ResizableCard>
          )}
        </div>
      </div>
    </div>
  );
}

export default App;

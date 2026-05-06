import axios from 'axios';
import type { Session, Message, LLMConfig, EmbeddingConfig, Agent, AgentWithSessions, Interaction, SearchConfig } from '../types';

declare global {
  interface Window {
    electronAPI?: {
      getServerPort: () => Promise<number>;
      getAppVersion: () => Promise<string>;
      isPackaged: () => Promise<boolean>;
      getPlatform: () => Promise<string>;
      onBackendStatus: (callback: (status: string) => void) => () => void;
      onBackendError: (callback: (error: string) => void) => () => void;
    };
  }
}

const DEFAULT_PORT = 8000;
const SERVER_HOST = '127.0.0.1';

let resolvedPort: number | null = null;
let portPromise: Promise<number> | null = null;

function resolvePort(): Promise<number> {
  if (resolvedPort !== null) return Promise.resolve(resolvedPort);
  if (portPromise) return portPromise;

  portPromise = (async () => {
    try {
      const hasApi = !!window.electronAPI;
      console.log('[api] electronAPI available:', hasApi);
      const port = await window.electronAPI?.getServerPort();
      console.log('[api] got port from IPC:', port);
      if (port && port > 0) {
        resolvedPort = port;
        console.log('[api] resolved dynamic port:', port);
        return port;
      }
      console.warn('[api] IPC returned invalid port, falling back to default');
    } catch (err) {
      console.warn('[api] getServerPort failed (non-Electron env?):', err);
    }
    resolvedPort = DEFAULT_PORT;
    console.log('[api] using default port:', DEFAULT_PORT);
    return DEFAULT_PORT;
  })();

  return portPromise;
}

function getApiBaseUrl(): string {
  const port = resolvedPort ?? DEFAULT_PORT;
  return `http://${SERVER_HOST}:${port}/api`;
}

function getServerBaseUrl(): string {
  const port = resolvedPort ?? DEFAULT_PORT;
  return `http://${SERVER_HOST}:${port}`;
}

export const API_BASE_URL = getApiBaseUrl();
export const SERVER_BASE_URL = getServerBaseUrl();

export function getDynamicApiBaseUrl(): string {
  const port = resolvedPort ?? DEFAULT_PORT;
  return `http://${SERVER_HOST}:${port}/api`;
}

export function getDynamicServerBaseUrl(): string {
  const port = resolvedPort ?? DEFAULT_PORT;
  return `http://${SERVER_HOST}:${port}`;
}

const api = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
});

api.interceptors.request.use(async (config) => {
  const port = await resolvePort();
  const url = `http://${SERVER_HOST}:${port}/api`;
  config.baseURL = url;
  return config;
});

export async function initApiClient(): Promise<number> {
  return resolvePort();
}

export const sessionApi = {
  list: () => api.get<Session[]>('/sessions'),
  get: (id: number) => api.get<Session>(`/sessions/${id}`),
  create: (data: Partial<Session>) => api.post<Session>('/sessions', data),
  update: (id: number, data: Partial<Session>) => api.put<Session>(`/sessions/${id}`, data),
  delete: (id: number) => api.delete(`/sessions/${id}`),
};

export const messageApi = {
  list: (sessionId: number) => api.get<Message[]>(`/messages/${sessionId}`),
  send: (sessionId: number, content: string) => 
    api.post<{trigger_message_id: number}>(`/chat/send/${sessionId}?message=${encodeURIComponent(content)}`),
  createAndSend: (content: string, agentId?: number, title?: string) =>
    api.post<{session_id: number, trigger_message_id: number}>(`/chat/new?message=${encodeURIComponent(content)}${agentId ? `&agent_id=${agentId}` : ''}${title ? `&title=${encodeURIComponent(title)}` : ''}`),
};

export const llmConfigApi = {
  list: () => api.get<LLMConfig[]>('/llm-configs'),
  get: (id: number) => api.get<LLMConfig>(`/llm-configs/${id}`),
  create: (data: Partial<LLMConfig>) => api.post<LLMConfig>('/llm-configs', data),
  update: (id: number, data: Partial<LLMConfig>) => api.put<LLMConfig>(`/llm-configs/${id}`, data),
  delete: (id: number) => api.delete(`/llm-configs/${id}`),
};

export const embeddingConfigApi = {
  list: () => api.get<EmbeddingConfig[]>('/embedding-configs'),
  get: (id: number) => api.get<EmbeddingConfig>(`/embedding-configs/${id}`),
  create: (data: Partial<EmbeddingConfig>) => api.post<EmbeddingConfig>('/embedding-configs', data),
  update: (id: number, data: Partial<EmbeddingConfig>) => api.put<EmbeddingConfig>(`/embedding-configs/${id}`, data),
  delete: (id: number) => api.delete(`/embedding-configs/${id}`),
};

export const agentApi = {
  list: () => api.get<Agent[]>('/agents'),
  listWithSessions: () => api.get<AgentWithSessions[]>('/agents/with-sessions'),
  get: (id: number) => api.get<Agent>(`/agents/${id}`),
  create: (data: Partial<Agent>) => api.post<Agent>('/agents', data),
  update: (id: number, data: Partial<Agent>) => api.put<Agent>(`/agents/${id}`, data),
  delete: (id: number) => api.delete(`/agents/${id}`),
};

export const interactionApi = {
  getInteractionStatus: (messageId: number) =>
    api.get<{ has_interactions: number }>(`/messages/${messageId}/interaction-status`),
  getInteractions: (agentMsgId: number) =>
    api.get<{ interactions: Interaction[] }>('/interactions', { params: { agent_msg_id: agentMsgId } }),
};

export const searchConfigApi = {
  get: () => api.get<SearchConfig>('/search-config'),
  update: (data: Partial<SearchConfig>) => api.put<SearchConfig>('/search-config', data),
};

export const uploadApi = {
  uploadAvatar: (agentId: number, file: File) => {
    const formData = new FormData();
    formData.append('file', file);
    return api.post<{ filename: string }>(`/uploads/avatar?agent_id=${agentId}`, formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    });
  },
};

export const getAvatarUrl = (avatar: string) => {
  if (!avatar) return '';
  return `${getDynamicServerBaseUrl()}/avatars/${avatar}`;
};

export const versionApi = {
  get: () => api.get<{ version: string }>('/version'),
};

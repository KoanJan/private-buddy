import axios from 'axios';
import type { Session, Message, LLMConfig, Agent, AgentWithSessions } from '../types';

const API_BASE_URL = 'http://localhost:8000/api';

const api = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
});

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
    api.post<{user_message_id: number, ai_message_id: number}>(`/chat/send/${sessionId}?message=${encodeURIComponent(content)}`),
  createAndSend: (content: string, agentId?: number, title?: string) =>
    api.post<{session_id: number, user_message_id: number, ai_message_id: number}>(`/chat/new?message=${encodeURIComponent(content)}${agentId ? `&agent_id=${agentId}` : ''}${title ? `&title=${encodeURIComponent(title)}` : ''}`),
};

export const llmConfigApi = {
  list: () => api.get<LLMConfig[]>('/llm-configs'),
  get: (id: number) => api.get<LLMConfig>(`/llm-configs/${id}`),
  create: (data: Partial<LLMConfig>) => api.post<LLMConfig>('/llm-configs', data),
  update: (id: number, data: Partial<LLMConfig>) => api.put<LLMConfig>(`/llm-configs/${id}`, data),
  delete: (id: number) => api.delete(`/llm-configs/${id}`),
};

export const agentApi = {
  list: () => api.get<Agent[]>('/agents'),
  listWithSessions: () => api.get<AgentWithSessions[]>('/agents/with-sessions'),
  get: (id: number) => api.get<Agent>(`/agents/${id}`),
  create: (data: Partial<Agent>) => api.post<Agent>('/agents', data),
  update: (id: number, data: Partial<Agent>) => api.put<Agent>(`/agents/${id}`, data),
  delete: (id: number) => api.delete(`/agents/${id}`),
};
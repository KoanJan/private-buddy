export interface Session {
  id: number;
  title: string;
  agent_id: number;
  status: number;
  created_at: string;
  updated_at: string | null;
}

export interface Message {
  id: number;
  session_id: number;
  role: 'user' | 'assistant';
  content: string;
  status: number;
  created_at: string;
  updated_at: string | null;
}

export interface LLMConfig {
  id: number;
  name: string;
  model_id: string;
  base_url: string;
  api_key: string;
  description: string;
  created_at: string;
  updated_at: string | null;
}

export interface EmbeddingConfig {
  id: number;
  name: string;
  model_id: string;
  base_url: string;
  api_key: string;
  description: string;
  created_at: string;
  updated_at: string | null;
}

export interface Agent {
  id: number;
  name: string;
  character_settings: string;
  llm_config_id: number;
  embedding_config_id: number;
  description: string;
  created_at: string;
  updated_at: string | null;
}

export interface SessionBrief {
  id: number;
  title: string;
  status: number;
  created_at: string;
  updated_at: string | null;
}

export interface AgentWithSessions extends Agent {
  sessions: SessionBrief[];
}

export const SESSION_STATUS_STREAMING = 0;
export const SESSION_STATUS_IDLE = 1;

export const MESSAGE_STATUS_STREAMING = 0;
export const MESSAGE_STATUS_COMPLETED = 1;
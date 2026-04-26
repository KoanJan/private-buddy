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
  has_interactions: number;
  created_at: string;
  updated_at: string | null;
}

export const HAS_INTERACTIONS_PENDING = 0;
export const HAS_INTERACTIONS_EXISTS = 1;
export const HAS_INTERACTIONS_NONE = 2;

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

export interface Interaction {
  id: number;
  session_id: number;
  user_msg_id: number;
  agent_msg_id: number;
  iteration: number;
  type: number;
  timestamp: string;
  data: string;
  created_at: string;
}

export const INTERACTION_TYPE_REQUEST = 1;
export const INTERACTION_TYPE_RESPONSE = 2;

export interface SearchConfig {
  id: number;
  provider: string;
  api_key: string;
  description: string;
  is_active: boolean;
  updated_at: string | null;
}

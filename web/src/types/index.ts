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
  avatar: string;
  knowledge_base_ids: number[];
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

// Session with agent info for flat session list display
export interface SessionWithAgent extends SessionBrief {
  agent_id: number;
  agent_name: string;
  agent_avatar: string;
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
  updated_at: string;
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

export interface KnowledgeBase {
  id: number;
  name: string;
  description: string;
  embedding_config_id: number;
  index_type: string;
  index_file_path: string;
  document_count: number;
  vector_count: number;
  deleted_count: number;
  created_at: string;
  updated_at: string;
}

export interface Document {
  id: number;
  knowledge_base_id: number;
  title: string;
  source: string;
  file_path: string;
  file_size: number;
  file_type: string;
  chunk_count: number;
  status: string;
  error_message: string;
  created_at: string;
  updated_at: string;
}

export interface SearchResult {
  chunk_id: number;
  document_id: number;
  document_title: string;
  content: string;
  score: number;
  knowledge_base_id: number;
}

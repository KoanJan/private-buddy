export interface Session {
  id: number;
  title: string;
  agent_id: number;
  created_at: string;
  updated_at: string | null;
}

// Message role constants (must match backend model.MessageRole*)
export const MESSAGE_ROLE_USER = 1;
export const MESSAGE_ROLE_ASSISTANT = 2;

export interface Message {
  id: number;
  session_id: number;
  role: number; // 1=user, 2=assistant
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
  description: string;
  avatar: string;
  knowledge_base_ids: number[];
  created_at: string;
  updated_at: string | null;
}

export interface SessionBrief {
  id: number;
  title: string;
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

// Knowledge base index type constants (must match backend model.KnowledgeBaseIndexType*)
export const KB_INDEX_TYPE_FLAT = 0;
export const KB_INDEX_TYPE_SWITCHING = 1;
export const KB_INDEX_TYPE_HNSW = 2;

export interface KnowledgeBase {
  id: number;
  name: string;
  description: string;
  index_type: number; // 0=flat, 1=switching, 2=hnsw
  index_file_path: string;
  document_count: number;
  vector_count: number;
  deleted_count: number;
  created_at: string;
  updated_at: string;
}

// Document status constants (must match backend model.DocumentStatus*)
export const DOC_STATUS_PENDING = 0;
export const DOC_STATUS_PROCESSING = 1;
export const DOC_STATUS_READY = 2;
export const DOC_STATUS_FAILED = 3;
export const DOC_STATUS_DELETED = 4;

export interface Document {
  id: number;
  knowledge_base_id: number;
  title: string;
  source: string;
  file_path: string;
  file_size: number;
  file_type: string;
  chunk_count: number;
  status: number; // 0=pending, 1=processing, 2=ready, 3=failed, 4=deleted
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

// Participant status constants (must match backend model.ParticipantStatus*)
export const PARTICIPANT_STATUS_IDLE = 0;
export const PARTICIPANT_STATUS_WORKING = 1;

export interface SessionAgentStatus {
  agent_id: number;
  name: string;
  avatar: string;
  status: number; // 0=idle, 1=working
}

export interface UserProfile {
  id: number;
  name: string;
  bio: string;
  created_at: string;
  updated_at: string;
}

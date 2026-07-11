import ConfigList from './ConfigList';
import { llmConfigApi } from '../services/api';
import type { LLMConfig } from '../types';

const FORM_FIELDS = [
  { name: 'name', labelKey: 'name', placeholderKey: 'namePlaceholder', required: true },
  { name: 'model_id', labelKey: 'modelId', placeholderKey: 'modelIdPlaceholder', required: true },
  { name: 'base_url', labelKey: 'baseUrl', placeholderKey: 'baseUrlPlaceholder', required: true },
  { name: 'api_key', labelKey: 'apiKey', placeholderKey: 'apiKeyPlaceholder', required: true, type: 'password' as const },
  { name: 'description', labelKey: 'description', placeholderKey: 'descriptionPlaceholder', type: 'textarea' as const, rows: 2 },
];

interface LLMConfigListProps {
  onSelectConfig?: (config: LLMConfig | null) => void;
  showCreate?: boolean;
  onCreateClose?: () => void;
  onConfigChanged?: () => void;
  beforeDelete?: (id: number) => Promise<boolean>;
}

/** LLM configuration list component wrapping ConfigList with LLM-specific fields. */
export default function LLMConfigList({ onSelectConfig, showCreate, onCreateClose, onConfigChanged, beforeDelete }: LLMConfigListProps) {
  return (
    <ConfigList<LLMConfig>
      api={llmConfigApi}
      formFields={FORM_FIELDS}
      i18nPrefix="llmConfig"
      primaryField="model_id"
      secondaryField="name"
      gridClassName="list-grid-3"
      onSelectConfig={onSelectConfig}
      showCreate={showCreate}
      onCreateClose={onCreateClose}
      onConfigChanged={onConfigChanged}
      beforeDelete={beforeDelete}
    />
  );
}

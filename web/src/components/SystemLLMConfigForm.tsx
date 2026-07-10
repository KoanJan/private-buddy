import React, { useState, useEffect } from 'react';
import { Form, Select, Button, message, Spin } from 'antd';
import { SaveOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import type { LLMConfig } from '../types';
import { systemLLMConfigApi, llmConfigApi } from '../services/api';
import { logger } from '../logger';
import { useConfigForm } from '../hooks/useConfigForm';

interface SystemLLMConfig { llm_config_id: number }

const SystemLLMConfigForm: React.FC<{ onSaved?: () => void; refreshKey?: number }> = ({ onSaved, refreshKey }) => {
  const { t } = useTranslation();
  const [form] = Form.useForm();
  const [llmConfigs, setLlmConfigs] = useState<LLMConfig[]>([]);

  const { loading, saving, dirty, handleSave, markDirty } = useConfigForm<SystemLLMConfig>({
    // loadApi is a no-op here — we do parallel loading below
    loadApi: async () => null,
    saveApi: (v) => systemLLMConfigApi.update(v as { llm_config_id: number }),
    i18nPrefix: 'systemLLMConfig',
    skipInitialLoad: true,
    onSaved,
    onLoaded: (data) => form.setFieldsValue(data),
  });

  useEffect(() => {
    let cancelled = false;
    const loadData = async () => {
      setLlmConfigs([]);
      try {
        const [sysRes, llmRes] = await Promise.all([
          systemLLMConfigApi.get(),
          llmConfigApi.list(),
        ]);
        if (cancelled) return;
        setLlmConfigs(llmRes.data);
        if (sysRes.data?.llm_config_id) {
          form.setFieldsValue({ llm_config_id: sysRes.data.llm_config_id });
        }
      } catch (error) {
        if (cancelled) return;
        logger.error('Failed to load system LLM config:', error);
        message.error(t('systemLLMConfig.loadError'));
      }
    };
    loadData();
    return () => { cancelled = true; };
  }, [refreshKey]);

  if (loading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', padding: '40px' }}>
        <Spin />
      </div>
    );
  }

  return (
    <div className="config-form-container">
      <p style={{ color: 'var(--color-text-secondary)', marginBottom: 24, lineHeight: 1.6 }}>
        {t('systemLLMConfig.description')}
      </p>
      <Form
        form={form}
        layout="vertical"
        onFinish={handleSave}
        onValuesChange={markDirty}
      >
        <Form.Item
          name="llm_config_id"
          label={t('systemLLMConfig.llmConfigId')}
          rules={[{ required: true, message: t('systemLLMConfig.llmConfigIdPlaceholder') }]}
        >
          <Select
            placeholder={t('systemLLMConfig.llmConfigIdPlaceholder')}
            options={llmConfigs.map(c => ({
              value: c.id,
              label: c.name,
            }))}
          />
        </Form.Item>

        <Form.Item style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 0 }}>
          <Button
            type="primary"
            htmlType="submit"
            icon={<SaveOutlined />}
            loading={saving}
            disabled={!dirty}
          >
            {t('common.save')}
          </Button>
        </Form.Item>
      </Form>
    </div>
  );
};

export default SystemLLMConfigForm;

import React, { useState, useEffect } from 'react';
import { Form, Select, Button, message, Spin } from 'antd';
import { SaveOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import type { LLMConfig } from '../types';
import { systemLLMConfigApi, llmConfigApi } from '../services/api';
import { logger } from '../logger';

const SystemLLMConfigForm: React.FC<{ onSaved?: () => void }> = ({ onSaved }) => {
  const { t } = useTranslation();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [llmConfigs, setLlmConfigs] = useState<LLMConfig[]>([]);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    loadData();
  }, []);

  const loadData = async () => {
    setLoading(true);
    try {
      const [sysRes, llmRes] = await Promise.all([
        systemLLMConfigApi.get(),
        llmConfigApi.list(),
      ]);
      setLlmConfigs(llmRes.data);
      if (sysRes.data?.llm_config_id) {
        form.setFieldsValue({ llm_config_id: sysRes.data.llm_config_id });
        setDirty(false);
      }
    } catch (error) {
      logger.error('Failed to load system LLM config:', error);
      message.error(t('systemLLMConfig.loadError'));
    } finally {
      setLoading(false);
    }
  };

  const handleSave = async (values: { llm_config_id: number }) => {
    setSaving(true);
    try {
      await systemLLMConfigApi.update(values);
      setDirty(false);
      message.success(t('systemLLMConfig.saveSuccess'));
      onSaved?.();
    } catch (error) {
      logger.error('Failed to save system LLM config:', error);
      message.error(t('systemLLMConfig.saveError'));
    } finally {
      setSaving(false);
    }
  };

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
        onValuesChange={() => setDirty(true)}
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

import React, { useState, useEffect } from 'react';
import { Form, Input, Select, Switch, Button, message, Spin } from 'antd';
import { SaveOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import type { SearchConfig } from '../types';
import { searchConfigApi } from '../services/api';
import { logger } from '../logger';

const { TextArea } = Input;

const SearchConfigForm: React.FC = () => {
  const { t } = useTranslation();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [config, setConfig] = useState<SearchConfig | null>(null);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    loadConfig();
  }, []);

  const loadConfig = async () => {
    setLoading(true);
    try {
      const response = await searchConfigApi.get();
      setConfig(response.data);
      form.setFieldsValue(response.data);
      setDirty(false);
    } catch (error) {
      logger.error('Failed to load search config:', error);
      message.error(t('searchConfig.loadError'));
    } finally {
      setLoading(false);
    }
  };

  const handleSave = async (values: Partial<SearchConfig>) => {
    if (values.is_active && !values.api_key) {
      message.error(t('searchConfig.apiKeyRequired'));
      return;
    }

    setSaving(true);
    try {
      const response = await searchConfigApi.update(values);
      setConfig(response.data);
      setDirty(false);
      message.success(t('searchConfig.saveSuccess'));
    } catch (error) {
      logger.error('Failed to save search config:', error);
      message.error(t('searchConfig.saveError'));
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
      <Form
        form={form}
        layout="vertical"
        onFinish={handleSave}
        initialValues={config || undefined}
        onValuesChange={() => setDirty(true)}
      >
        <Form.Item
          name="provider"
          label={t('searchConfig.provider')}
        >
          <Select>
            <Select.Option value="tavily">Tavily</Select.Option>
            <Select.Option value="duckduckgo">DuckDuckGo</Select.Option>
          </Select>
        </Form.Item>

        <Form.Item
          name="api_key"
          label={t('searchConfig.apiKey')}
        >
          <Input.Password placeholder={t('searchConfig.apiKeyPlaceholder')} />
        </Form.Item>

        <Form.Item
          name="description"
          label={t('searchConfig.description')}
        >
          <TextArea rows={3} placeholder={t('searchConfig.descriptionPlaceholder')} />
        </Form.Item>

        <Form.Item
          name="is_active"
          label={t('searchConfig.isActive')}
          valuePropName="checked"
        >
          <Switch />
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

export default SearchConfigForm;

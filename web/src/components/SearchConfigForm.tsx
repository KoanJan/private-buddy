import React, { useState, useEffect } from 'react';
import { Form, Input, Select, Switch, Button, message, Spin } from 'antd';
import { SaveOutlined, ReloadOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import type { SearchConfig } from '../types';
import { searchConfigApi } from '../services/api';

const { TextArea } = Input;

const SearchConfigForm: React.FC = () => {
  const { t } = useTranslation();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [config, setConfig] = useState<SearchConfig | null>(null);

  useEffect(() => {
    loadConfig();
  }, []);

  const loadConfig = async () => {
    setLoading(true);
    try {
      const response = await searchConfigApi.get();
      setConfig(response.data);
      form.setFieldsValue(response.data);
    } catch (error) {
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
      message.success(t('searchConfig.saveSuccess'));
    } catch (error) {
      message.error(t('searchConfig.saveError'));
    } finally {
      setSaving(false);
    }
  };

  const handleReset = () => {
    if (config) {
      form.setFieldsValue(config);
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

        <Form.Item>
          <Button
            type="primary"
            htmlType="submit"
            icon={<SaveOutlined />}
            loading={saving}
            style={{ marginRight: 8 }}
          >
            {t('common.save')}
          </Button>
          <Button
            icon={<ReloadOutlined />}
            onClick={handleReset}
          >
            {t('common.reset')}
          </Button>
        </Form.Item>
      </Form>
    </div>
  );
};

export default SearchConfigForm;

import React from 'react';
import { Form, Input, Select, Switch, Button, Spin } from 'antd';
import { SaveOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import type { SearchConfig } from '../types';
import { searchConfigApi } from '../services/api';
import { useConfigForm } from '../hooks/useConfigForm';

const { TextArea } = Input;

const SearchConfigForm: React.FC = () => {
  const { t } = useTranslation();
  const [form] = Form.useForm();
  const { loading, saving, dirty, config, handleSave, markDirty } = useConfigForm<SearchConfig>({
    loadApi: searchConfigApi.get,
    saveApi: searchConfigApi.update,
    i18nPrefix: 'searchConfig',
    beforeSave: (values) => {
      if (values.is_active && !values.api_key) return t('searchConfig.apiKeyRequired');
      return null;
    },
    onLoaded: (data) => form.setFieldsValue(data),
  });

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
        onValuesChange={markDirty}
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

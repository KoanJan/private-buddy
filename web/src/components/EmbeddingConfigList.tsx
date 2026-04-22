import React, { useEffect, useState } from 'react';
import { Button, Modal, Input, message, Form } from 'antd';
import { DeleteOutlined, EditOutlined, ApiOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import type { EmbeddingConfig } from '../types';
import { embeddingConfigApi } from '../services/api';
import { logger } from '../logger';
import { confirmDelete } from '../utils/confirm';

interface EmbeddingConfigListProps {
  onSelectConfig?: (config: EmbeddingConfig | null) => void;
  showCreate?: boolean;
  onCreateClose?: () => void;
}

const EmbeddingConfigList: React.FC<EmbeddingConfigListProps> = ({ onSelectConfig, showCreate, onCreateClose }) => {
  const { t } = useTranslation();
  const [configs, setConfigs] = useState<EmbeddingConfig[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalVisible, setModalVisible] = useState(false);
  const [editModalVisible, setEditModalVisible] = useState(false);
  const [form] = Form.useForm();
  const [editForm] = Form.useForm();
  const [editingConfig, setEditingConfig] = useState<EmbeddingConfig | null>(null);

  const loadConfigs = async () => {
    setLoading(true);
    try {
      const response = await embeddingConfigApi.list();
      setConfigs(response.data);
    } catch (error) {
      logger.error('Failed to load embedding configs:', error);
      message.error(t('messages.loadFailed'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadConfigs();
  }, []);

  useEffect(() => {
    if (showCreate) {
      setModalVisible(true);
    }
  }, [showCreate]);

  const handleModalClose = () => {
    setModalVisible(false);
    form.resetFields();
    if (onCreateClose) {
      onCreateClose();
    }
  };

  const handleCreateConfig = async (values: Record<string, unknown>) => {
    try {
      const response = await embeddingConfigApi.create(values);
      setConfigs([response.data, ...configs]);
      setModalVisible(false);
      form.resetFields();
      message.success(t('embeddingConfig.createSuccess'));
      if (onSelectConfig) {
        onSelectConfig(response.data);
      }
    } catch (error) {
      logger.error('Failed to create embedding config:', error);
      message.error(t('embeddingConfig.createFailed'));
    }
  };

  const handleUpdateConfig = async (values: Record<string, unknown>) => {
    if (!editingConfig) return;
    
    try {
      const response = await embeddingConfigApi.update(editingConfig.id, values);
      const index = configs.findIndex(c => c.id === editingConfig.id);
      if (index !== -1) {
        const newConfigs = [...configs];
        newConfigs[index] = response.data;
        setConfigs(newConfigs);
      }
      setEditModalVisible(false);
      editForm.resetFields();
      setEditingConfig(null);
      message.success(t('embeddingConfig.updateSuccess'));
      if (onSelectConfig) {
        onSelectConfig(response.data);
      }
    } catch (error) {
      logger.error('Failed to update embedding config:', error);
      message.error(t('embeddingConfig.updateFailed'));
    }
  };

  const handleDeleteConfig = async (configId: number, e: React.MouseEvent) => {
    e.stopPropagation();
    
    confirmDelete({
      title: t('embeddingConfig.confirmDeleteTitle'),
      content: t('embeddingConfig.confirmDelete'),
      okText: t('common.delete'),
      cancelText: t('common.cancel'),
      onOk: async () => {
        try {
          await embeddingConfigApi.delete(configId);
          setConfigs(configs.filter(c => c.id !== configId));
          message.success(t('embeddingConfig.deleteSuccess'));
          if (onSelectConfig && editingConfig?.id === configId) {
            onSelectConfig(null);
          }
        } catch (error) {
          logger.error('Failed to delete embedding config:', error);
          message.error(t('embeddingConfig.deleteFailed'));
        }
      },
    });
  };

  const handleEditConfig = (config: EmbeddingConfig) => {
    setEditingConfig(config);
    editForm.setFieldsValue({
      name: config.name,
      model_id: config.model_id,
      base_url: config.base_url,
      api_key: config.api_key,
      description: config.description || '',
    });
    setEditModalVisible(true);
  };

  return (
    <>
      <div style={{ minHeight: '400px', maxHeight: '600px', overflowY: 'auto' }}>
        {loading ? (
          <div style={{ textAlign: 'center', padding: '20px', color: '#9ca3af' }}>
            {t('sidebar.loading')}
          </div>
        ) : configs.length === 0 ? (
          <div style={{ textAlign: 'center', padding: '20px', color: '#9ca3af' }}>
            {t('embeddingConfig.noConfig')}
          </div>
      ) : (
        configs.map((config) => (
          <div
            key={config.id}
            className="session-item"
            style={{ cursor: 'default' }}
          >
            <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between' }}>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div className="session-title">
                  <ApiOutlined style={{ marginRight: '8px', fontSize: '12px' }} />
                  {config.name}
                </div>
                <div style={{ fontSize: '12px', color: '#9ca3af', marginTop: '4px' }}>
                  {config.model_id}
                </div>
              </div>
              <div style={{ display: 'flex', gap: '4px', marginLeft: '8px' }}>
                <Button
                  type="text"
                  size="small"
                  icon={<EditOutlined />}
                  onClick={(e) => {
                    e.stopPropagation();
                    handleEditConfig(config);
                  }}
                  style={{ color: '#6b7280' }}
                />
                <Button
                  type="text"
                  size="small"
                  danger
                  icon={<DeleteOutlined />}
                  onClick={(e) => handleDeleteConfig(config.id, e)}
                />
              </div>
            </div>
          </div>
        ))
      )}
      </div>

      <Modal
        title={t('embeddingConfig.create')}
        open={modalVisible}
        onOk={() => form.submit()}
        onCancel={handleModalClose}
        okText={t('common.create')}
        cancelText={t('common.cancel')}
      >
        <Form
          form={form}
          layout="vertical"
          name="embedding_config_form"
          onFinish={handleCreateConfig}
          style={{ marginTop: '16px' }}
        >
          <Form.Item
            label={t('embeddingConfig.name')}
            name="name"
            rules={[{ required: true, message: t('embeddingConfig.namePlaceholder') }]}
          >
            <Input placeholder={t('embeddingConfig.namePlaceholder')} />
          </Form.Item>
          
          <Form.Item
            label={t('embeddingConfig.modelId')}
            name="model_id"
            rules={[{ required: true, message: t('embeddingConfig.modelIdPlaceholder') }]}
          >
            <Input placeholder={t('embeddingConfig.modelIdPlaceholder')} />
          </Form.Item>
          
          <Form.Item
            label={t('embeddingConfig.baseUrl')}
            name="base_url"
            rules={[{ required: true, message: t('embeddingConfig.baseUrlPlaceholder') }]}
          >
            <Input placeholder={t('embeddingConfig.baseUrlPlaceholder')} />
          </Form.Item>
          
          <Form.Item
            label={t('embeddingConfig.apiKey')}
            name="api_key"
            rules={[{ required: true, message: t('embeddingConfig.apiKeyPlaceholder') }]}
          >
            <Input.Password placeholder={t('embeddingConfig.apiKeyPlaceholder')} />
          </Form.Item>
          
          <Form.Item
            label={t('embeddingConfig.description')}
            name="description"
          >
            <Input.TextArea placeholder={t('embeddingConfig.descriptionPlaceholder')} rows={2} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={t('embeddingConfig.edit')}
        open={editModalVisible}
        onOk={() => editForm.submit()}
        onCancel={() => {
          setEditModalVisible(false);
          editForm.resetFields();
          setEditingConfig(null);
        }}
        okText={t('common.update')}
        cancelText={t('common.cancel')}
      >
        <Form
          form={editForm}
          layout="vertical"
          name="embedding_config_edit_form"
          onFinish={handleUpdateConfig}
          style={{ marginTop: '16px' }}
        >
          <Form.Item
            label={t('embeddingConfig.name')}
            name="name"
            rules={[{ required: true, message: t('embeddingConfig.namePlaceholder') }]}
          >
            <Input placeholder={t('embeddingConfig.namePlaceholder')} />
          </Form.Item>
          
          <Form.Item
            label={t('embeddingConfig.modelId')}
            name="model_id"
            rules={[{ required: true, message: t('embeddingConfig.modelIdPlaceholder') }]}
          >
            <Input placeholder={t('embeddingConfig.modelIdPlaceholder')} />
          </Form.Item>
          
          <Form.Item
            label={t('embeddingConfig.baseUrl')}
            name="base_url"
            rules={[{ required: true, message: t('embeddingConfig.baseUrlPlaceholder') }]}
          >
            <Input placeholder={t('embeddingConfig.baseUrlPlaceholder')} />
          </Form.Item>
          
          <Form.Item
            label={t('embeddingConfig.apiKey')}
            name="api_key"
            rules={[{ required: true, message: t('embeddingConfig.apiKeyPlaceholder') }]}
          >
            <Input.Password placeholder={t('embeddingConfig.apiKeyPlaceholder')} />
          </Form.Item>
          
          <Form.Item
            label={t('embeddingConfig.description')}
            name="description"
          >
            <Input.TextArea placeholder={t('embeddingConfig.descriptionPlaceholder')} rows={2} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
};

export default EmbeddingConfigList;

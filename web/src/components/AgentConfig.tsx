import React, { useEffect, useState } from 'react';
import { Button, Modal, Form, Input, message, Select, Upload } from 'antd';
import { EditOutlined, DeleteOutlined, PlusOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import type { Agent, LLMConfig } from '../types';
import { agentApi, llmConfigApi, uploadApi } from '../services/api';
import { logger } from '../logger';
import { confirmDelete } from '../utils/confirm';
import AgentAvatar from './AgentAvatar';

interface AgentConfigProps {
  showCreate?: boolean;
  onCreateClose?: () => void;
  onAgentCreated?: () => void;
}

const AgentConfig: React.FC<AgentConfigProps> = ({ showCreate, onCreateClose, onAgentCreated }) => {
  const { t } = useTranslation();
  const [agents, setAgents] = useState<Agent[]>([]);
  const [llmConfigs, setLLMConfigs] = useState<LLMConfig[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalVisible, setModalVisible] = useState(false);
  const [editModalVisible, setEditModalVisible] = useState(false);
  const [editingAgent, setEditingAgent] = useState<Agent | null>(null);
  const [form] = Form.useForm();
  const [editForm] = Form.useForm();
  const [createAvatarFile, setCreateAvatarFile] = useState<File | null>(null);
  const [createAvatarPreview, setCreateAvatarPreview] = useState<string>('');
  const [editAvatarFile, setEditAvatarFile] = useState<File | null>(null);
  const [editAvatarPreview, setEditAvatarPreview] = useState<string>('');

  const loadAgents = async () => {
    setLoading(true);
    try {
      const response = await agentApi.list();
      setAgents(response.data);
    } catch (error) {
      message.error(t('messages.loadFailed'));
    } finally {
      setLoading(false);
    }
  };

  const loadLLMConfigs = async () => {
    try {
      const response = await llmConfigApi.list();
      setLLMConfigs(response.data);
    } catch (error) {
      logger.error('Failed to load LLM configs:', error);
    }
  };

  useEffect(() => {
    loadAgents();
    loadLLMConfigs();
  }, []);

  useEffect(() => {
    if (showCreate) {
      setModalVisible(true);
    }
  }, [showCreate]);

  const handleModalClose = () => {
    setModalVisible(false);
    form.resetFields();
    setCreateAvatarFile(null);
    setCreateAvatarPreview('');
    if (onCreateClose) {
      onCreateClose();
    }
  };

  const handleCreateAgent = async (values: Record<string, unknown>) => {
    try {
      const response = await agentApi.create(values);
      const newAgent = response.data;

      if (createAvatarFile) {
        try {
          const uploadRes = await uploadApi.uploadAvatar(newAgent.id, createAvatarFile);
          newAgent.avatar = uploadRes.data.filename;
        } catch (error) {
          logger.error('Failed to upload avatar:', error);
        }
      }

      setAgents([newAgent, ...agents]);
      setModalVisible(false);
      form.resetFields();
      setCreateAvatarFile(null);
      setCreateAvatarPreview('');
      message.success(t('agent.createSuccess'));
      if (onAgentCreated) {
        onAgentCreated();
      }
    } catch (error) {
      logger.error('Failed to create agent:', error);
      message.error(t('agent.createFailed'));
    }
  };

  const handleUpdateAgent = async (values: Record<string, unknown>) => {
    if (!editingAgent) return;

    try {
      let avatarFilename = editingAgent.avatar;

      if (editAvatarFile) {
        try {
          const uploadRes = await uploadApi.uploadAvatar(editingAgent.id, editAvatarFile);
          avatarFilename = uploadRes.data.filename;
        } catch (error) {
          logger.error('Failed to upload avatar:', error);
        }
      }

      const updateData = { ...values, avatar: avatarFilename };
      const response = await agentApi.update(editingAgent.id, updateData);
      const index = agents.findIndex(a => a.id === editingAgent.id);
      if (index !== -1) {
        const newAgents = [...agents];
        newAgents[index] = response.data;
        setAgents(newAgents);
      }
      setEditModalVisible(false);
      editForm.resetFields();
      setEditingAgent(null);
      setEditAvatarFile(null);
      setEditAvatarPreview('');
      message.success(t('agent.updateSuccess'));
    } catch (error) {
      logger.error('Failed to update agent:', error);
      message.error(t('agent.updateFailed'));
    }
  };

  const handleDeleteAgent = async (agentId: number, e: React.MouseEvent) => {
    e.stopPropagation();

    confirmDelete({
      title: t('agent.confirmDeleteTitle'),
      content: t('agent.confirmDelete'),
      okText: t('common.delete'),
      cancelText: t('common.cancel'),
      onOk: async () => {
        try {
          await agentApi.delete(agentId);
          setAgents(agents.filter(a => a.id !== agentId));
          message.success(t('agent.deleteSuccess'));
        } catch (error) {
          logger.error('Failed to delete agent:', error);
          message.error(t('agent.deleteFailed'));
        }
      },
    });
  };

  const handleEditAgent = (agent: Agent) => {
    setEditingAgent(agent);
    setEditAvatarFile(null);
    setEditAvatarPreview('');
    editForm.setFieldsValue({
      name: agent.name,
      character_settings: agent.character_settings || '',
      description: agent.description || '',
      llm_config_id: agent.llm_config_id,
    });
    setEditModalVisible(true);
  };

  const renderAvatarUpload = (
    setAvatarFile: (f: File | null) => void,
    setAvatarPreview: (url: string) => void,
    currentAvatar?: string,
    previewUrl?: string,
  ) => {
    const showImage = previewUrl || currentAvatar;

    return (
      <Upload
        accept=".jpg,.jpeg,.png,.webp"
        showUploadList={false}
        beforeUpload={(file) => {
          setAvatarFile(file);
          setAvatarPreview(URL.createObjectURL(file));
          return false;
        }}
      >
        {showImage ? (
          <div className="avatar-upload-preview">
            {previewUrl ? (
              <img
                src={previewUrl}
                alt="preview"
                className="avatar-upload-preview-img"
              />
            ) : (
              <AgentAvatar
                avatar={currentAvatar || ''}
                size={64}
                borderRadius="50%"
                iconSize={28}
              />
            )}
            <div className="avatar-upload-overlay">
              <PlusOutlined />
            </div>
          </div>
        ) : (
          <div className="avatar-upload-trigger">
            <PlusOutlined style={{ fontSize: '20px', color: 'var(--color-text-placeholder)' }} />
            <div style={{ marginTop: '4px', fontSize: '12px', color: 'var(--color-text-placeholder)' }}>
              {t('agent.avatarUpload')}
            </div>
          </div>
        )}
      </Upload>
    );
  };

  return (
    <>
      <div className="agent-card-grid">
        {loading ? (
          <div className="empty-state-text">
            {t('sidebar.loading')}
          </div>
        ) : agents.length === 0 ? (
          <div className="empty-state-text">
            {t('sidebar.noAgent')}
          </div>
        ) : (
          agents.map((agent) => (
            <div
              key={agent.id}
              className="agent-card agent-card-block"
            >
              <AgentAvatar avatar={agent.avatar} size={44} iconSize={20} borderRadius="10px" />
              <div className="agent-card-block-name">{agent.name}</div>
              <div className="agent-card-block-desc">{agent.description || t('agent.noDescription')}</div>
              <div className="item-actions agent-card-block-actions">
                <Button
                  type="text"
                  size="small"
                  icon={<EditOutlined />}
                  onClick={(e) => {
                    e.stopPropagation();
                    handleEditAgent(agent);
                  }}
                  style={{ color: 'var(--color-text-secondary)' }}
                />
                <Button
                  type="text"
                  size="small"
                  danger
                  icon={<DeleteOutlined />}
                  onClick={(e) => handleDeleteAgent(agent.id, e)}
                />
              </div>
            </div>
          ))
        )}
      </div>

      <Modal
        title={t('agent.create')}
        open={modalVisible}
        onOk={() => form.submit()}
        onCancel={handleModalClose}
        okText={t('common.create')}
        cancelText={t('common.cancel')}
        width={600}
      >
        <Form
          form={form}
          layout="vertical"
          name="agent_form"
          onFinish={handleCreateAgent}
          style={{ marginTop: '16px' }}
        >
          <Form.Item label={t('agent.avatar')}>
            {renderAvatarUpload(setCreateAvatarFile, setCreateAvatarPreview, undefined, createAvatarPreview)}
          </Form.Item>

          <Form.Item
            label={t('agent.name')}
            name="name"
            rules={[{ required: true, message: t('agent.namePlaceholder') }]}
          >
            <Input placeholder={t('agent.namePlaceholder')} />
          </Form.Item>

          <Form.Item
            label={t('agent.characterSettings')}
            name="character_settings"
          >
            <Input.TextArea
              placeholder={t('agent.characterSettingsPlaceholder')}
              rows={4}
            />
          </Form.Item>

          <Form.Item
            label={t('agent.description')}
            name="description"
          >
            <Input.TextArea
              placeholder={t('agent.descriptionPlaceholder')}
              rows={2}
            />
          </Form.Item>

          <Form.Item
            label={t('agent.llmConfigId')}
            name="llm_config_id"
            rules={[{ required: true, message: t('agent.llmConfigIdPlaceholder') }]}
          >
            <Select placeholder={t('agent.llmConfigIdPlaceholder')}>
              {llmConfigs.map(config => (
                <Select.Option key={config.id} value={config.id}>
                  {config.name}
                </Select.Option>
              ))}
            </Select>
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={t('agent.edit')}
        open={editModalVisible}
        onOk={() => editForm.submit()}
        onCancel={() => {
          setEditModalVisible(false);
          editForm.resetFields();
          setEditingAgent(null);
          setEditAvatarFile(null);
          setEditAvatarPreview('');
        }}
        okText={t('common.update')}
        cancelText={t('common.cancel')}
        width={600}
      >
        <Form
          form={editForm}
          layout="vertical"
          name="agent_edit_form"
          onFinish={handleUpdateAgent}
          style={{ marginTop: '16px' }}
        >
          <Form.Item label={t('agent.avatar')}>
            {renderAvatarUpload(
              setEditAvatarFile,
              setEditAvatarPreview,
              editingAgent?.avatar,
              editAvatarPreview,
            )}
          </Form.Item>

          <Form.Item
            label={t('agent.name')}
            name="name"
            rules={[{ required: true, message: t('agent.namePlaceholder') }]}
          >
            <Input placeholder={t('agent.namePlaceholder')} />
          </Form.Item>

          <Form.Item
            label={t('agent.characterSettings')}
            name="character_settings"
          >
            <Input.TextArea
              placeholder={t('agent.characterSettingsPlaceholder')}
              rows={4}
            />
          </Form.Item>

          <Form.Item
            label={t('agent.description')}
            name="description"
          >
            <Input.TextArea
              placeholder={t('agent.descriptionPlaceholder')}
              rows={2}
            />
          </Form.Item>

          <Form.Item
            label={t('agent.llmConfigId')}
            name="llm_config_id"
            rules={[{ required: true, message: t('agent.llmConfigIdPlaceholder') }]}
          >
            <Select placeholder={t('agent.llmConfigIdPlaceholder')}>
              {llmConfigs.map(config => (
                <Select.Option key={config.id} value={config.id}>
                  {config.name}
                </Select.Option>
              ))}
            </Select>
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
};

export default AgentConfig;

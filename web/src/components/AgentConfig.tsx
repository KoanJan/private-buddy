import React, { useEffect, useState } from 'react';
import { Button, Modal, Form, Input, message, Select } from 'antd';
import { EditOutlined, DeleteOutlined, RobotOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import type { Agent, LLMConfig } from '../types';
import { agentApi, llmConfigApi } from '../services/api';
import { logger } from '../logger';
import { confirmDelete } from '../utils/confirm';

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
    if (onCreateClose) {
      onCreateClose();
    }
  };

  const handleCreateAgent = async (values: Record<string, unknown>) => {
    try {
      const response = await agentApi.create(values);
      setAgents([response.data, ...agents]);
      setModalVisible(false);
      form.resetFields();
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
      const response = await agentApi.update(editingAgent.id, values);
      const index = agents.findIndex(a => a.id === editingAgent.id);
      if (index !== -1) {
        const newAgents = [...agents];
        newAgents[index] = response.data;
        setAgents(newAgents);
      }
      setEditModalVisible(false);
      editForm.resetFields();
      setEditingAgent(null);
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
    editForm.setFieldsValue({
      name: agent.name,
      system_prompt: agent.system_prompt || '',
      description: agent.description || '',
      llm_config_id: agent.llm_config_id,
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
        ) : agents.length === 0 ? (
          <div style={{ textAlign: 'center', padding: '20px', color: '#9ca3af' }}>
            {t('sidebar.noAgent')}
          </div>
        ) : (
          agents.map((agent) => (
            <div
              key={agent.id}
              className="session-item"
              style={{ cursor: 'default' }}
            >
              <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between' }}>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div className="session-title">
                    <RobotOutlined style={{ marginRight: '8px', fontSize: '12px' }} />
                    {agent.name}
                  </div>
                  <div style={{ fontSize: '12px', color: '#9ca3af', marginTop: '4px' }}>
                    {agent.description || t('agent.noDescription')}
                  </div>
                </div>
                <div style={{ display: 'flex', gap: '4px', marginLeft: '8px' }}>
                  <Button
                    type="text"
                    size="small"
                    icon={<EditOutlined />}
                    onClick={(e) => {
                      e.stopPropagation();
                      handleEditAgent(agent);
                    }}
                    style={{ color: '#6b7280' }}
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
          <Form.Item
            label={t('agent.name')}
            name="name"
            rules={[{ required: true, message: t('agent.namePlaceholder') }]}
          >
            <Input placeholder={t('agent.namePlaceholder')} />
          </Form.Item>
          
          <Form.Item
            label={t('agent.systemPrompt')}
            name="system_prompt"
          >
            <Input.TextArea 
              placeholder={t('agent.systemPromptPlaceholder')} 
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
          <Form.Item
            label={t('agent.name')}
            name="name"
            rules={[{ required: true, message: t('agent.namePlaceholder') }]}
          >
            <Input placeholder={t('agent.namePlaceholder')} />
          </Form.Item>
          
          <Form.Item
            label={t('agent.systemPrompt')}
            name="system_prompt"
          >
            <Input.TextArea 
              placeholder={t('agent.systemPromptPlaceholder')} 
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
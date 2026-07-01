import React, { useEffect, useState } from 'react';
import { Button, Modal, Form, Upload, message, Spin } from 'antd';
import { DeleteOutlined, UploadOutlined } from '@ant-design/icons';
import type { UploadFile } from 'antd';
import { useTranslation } from 'react-i18next';
import { confirmDelete } from '../utils/confirm';
import type { PublicExperience } from '../types';
import { publicExperienceApi } from '../services/api';

interface PublicExperienceListProps {
  showIngest?: boolean;
  onIngestClose?: () => void;
  onSelectExp?: (exp: PublicExperience) => void;
}

const PublicExperienceList: React.FC<PublicExperienceListProps> = ({ showIngest, onIngestClose, onSelectExp }) => {
  const { t } = useTranslation();
  const [experiences, setExperiences] = useState<PublicExperience[]>([]);
  const [loading, setLoading] = useState(false);
  const [ingestVisible, setIngestVisible] = useState(false);
  const [ingesting, setIngesting] = useState(false);
  const [ingestForm] = Form.useForm();
  const [fileContent, setFileContent] = useState<string | null>(null);
  const [fileName, setFileName] = useState<string | null>(null);

  useEffect(() => {
    loadExperiences();
  }, []);

  useEffect(() => {
    if (showIngest) {
      setIngestVisible(true);
    }
  }, [showIngest]);

  const closeIngest = () => {
    setIngestVisible(false);
    ingestForm.resetFields();
    setFileContent(null);
    setFileName(null);
    onIngestClose?.();
  };

  const loadExperiences = async () => {
    setLoading(true);
    try {
      const res = await publicExperienceApi.list();
      setExperiences(res.data);
    } catch {
      message.error(t('publicExperience.loadError'));
    } finally {
      setLoading(false);
    }
  };

  const handleDelete = async (id: number) => {
    confirmDelete({
      title: t('publicExperience.confirmDeleteTitle'),
      content: t('publicExperience.confirmDelete'),
      okText: t('common.delete'),
      cancelText: t('common.cancel'),
      onOk: async () => {
        try {
          await publicExperienceApi.delete(id);
          setExperiences(prev => prev.filter(e => e.id !== id));
          message.success(t('publicExperience.deleteSuccess'));
        } catch {
          message.error(t('publicExperience.deleteFailed'));
        }
      },
    });
  };

  const handleFileChange = (info: { fileList: UploadFile[] }) => {
    const file = info.fileList[0]?.originFileObj;
    if (!file) {
      setFileContent(null);
      setFileName(null);
      return;
    }
    const reader = new FileReader();
    reader.onload = () => {
      setFileName(file.name);
      setFileContent(reader.result as string);
    };
    reader.onerror = () => {
      message.error(t('publicExperience.readFileError'));
    };
    reader.readAsText(file);
  };

  const handleIngest = async () => {
    if (!fileContent || !fileName) return;

    setIngesting(true);
    try {
      await publicExperienceApi.ingest({
        source_name: fileName,
        raw_content: fileContent,
      });
      closeIngest();
      message.success(t('publicExperience.ingestSuccess'));
    } catch (err: unknown) {
      const detail = (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail;
      if (detail?.includes('already been ingested')) {
        message.warning(t('publicExperience.duplicateSkill'));
      } else {
        message.error(t('publicExperience.ingestFailed'));
      }
    } finally {
      setIngesting(false);
    }
  };

  const sourceLabel = (sourceType: number) =>
    sourceType === 1 ? t('publicExperience.sourceIngestion') : t('publicExperience.sourceShare');

  return (
    <>
      <div>
        {loading ? (
          <div style={{ display: 'flex', justifyContent: 'center', padding: '40px' }}>
            <Spin />
          </div>
        ) : experiences.length === 0 ? (
          <div className="empty-state-text">{t('publicExperience.noData')}</div>
        ) : (
          <div className="list-grid-2">
            {experiences.map(exp => (
              <div
                key={exp.id}
                className="item-card"
                style={{ cursor: 'pointer' }}
                onClick={() => onSelectExp?.(exp)}
              >
                <div className="item-card-header">
                  <div className="item-card-info" style={{ flex: 1 }}>
                    <div className="item-card-name">{exp.title}</div>
                    <div className="item-card-desc">
                      {exp.description}
                    </div>
                    <div className="item-card-desc" style={{ fontSize: '11px', opacity: 0.6 }}>
                      {sourceLabel(exp.source_type)} · {new Date(exp.created_at).toLocaleDateString()}
                    </div>
                  </div>
                  <Button
                    className="item-actions"
                    type="text"
                    size="small"
                    danger
                    icon={<DeleteOutlined />}
                    onClick={(e) => {
                      e.stopPropagation();
                      handleDelete(exp.id);
                    }}
                  />
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <Modal
        title={t('publicExperience.ingest')}
        open={ingestVisible}
        width={700}
        onCancel={closeIngest}
        onOk={handleIngest}
        confirmLoading={ingesting}
        okText={t('common.confirm')}
        cancelText={t('common.cancel')}
      >
        <Form
          form={ingestForm}
          layout="vertical"
          style={{ marginTop: '16px' }}
        >
          <Form.Item
            name="file"
            label={t('publicExperience.skillFile')}
            rules={[{ required: true, message: t('publicExperience.skillFileRequired') }]}
            valuePropName="fileList"
            getValueFromEvent={e => e?.fileList}
          >
            <Upload
              accept=".md"
              maxCount={1}
              beforeUpload={() => false}
              onChange={handleFileChange}
            >
              <Button icon={<UploadOutlined />}>{t('publicExperience.uploadHint')}</Button>
            </Upload>
          </Form.Item>

          {fileContent && (
            <Form.Item label={t('publicExperience.preview')}>
              <div style={{
                maxHeight: 300,
                overflow: 'auto',
                border: '1px solid var(--color-border)',
                borderRadius: 6,
                padding: '12px 16px',
                background: 'var(--color-bg-secondary)',
              }}>
                <pre style={{
                  margin: 0,
                  fontFamily: 'monospace',
                  fontSize: 12,
                  lineHeight: 1.5,
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-word',
                }}>
                  {fileContent}
                </pre>
              </div>
            </Form.Item>
          )}
        </Form>
      </Modal>
    </>
  );
};

export default PublicExperienceList;

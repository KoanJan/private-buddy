import React, { useEffect, useState } from 'react';
import { Button, Modal, Form, Upload, message, Spin, Tag } from 'antd';
import { DeleteOutlined, UploadOutlined, ReloadOutlined } from '@ant-design/icons';
import type { UploadFile } from 'antd';
import { useTranslation } from 'react-i18next';
import { confirmDelete } from '../utils/confirm';
import type { PublicExperience } from '../types';
import {
  PUBLIC_EXPERIENCE_STATUS_GENERATING,
  PUBLIC_EXPERIENCE_STATUS_ERROR,
} from '../types';
import { publicExperienceApi } from '../services/api';

interface PublicExperienceListProps {
  showIngest?: boolean;
  onIngestClose?: () => void;
  onSelectExp?: (exp: PublicExperience) => void;
}

// Poll interval for refreshing the list while any experience is still Generating.
const POLL_INTERVAL_MS = 3000;

const PublicExperienceList: React.FC<PublicExperienceListProps> = ({ showIngest, onIngestClose, onSelectExp }) => {
  const { t } = useTranslation();
  const [experiences, setExperiences] = useState<PublicExperience[]>([]);
  const [loading, setLoading] = useState(false);
  const [ingestVisible, setIngestVisible] = useState(false);
  const [ingesting, setIngesting] = useState(false);
  const [redistillingId, setRedistillingId] = useState<number | null>(null);
  const [ingestForm] = Form.useForm();
  const [fileContent, setFileContent] = useState<string | null>(null);
  const [fileName, setFileName] = useState<string | null>(null);

  // Load the experience list. When silent=true, no loading spinner / error toast
  // is shown — used by the polling loop to avoid disruptive UX.
  const loadExperiences = async (silent = false) => {
    if (!silent) setLoading(true);
    try {
      const res = await publicExperienceApi.list();
      setExperiences(res.data);
    } catch {
      if (!silent) message.error(t('publicExperience.loadError'));
    } finally {
      if (!silent) setLoading(false);
    }
  };

  useEffect(() => {
    loadExperiences();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Poll while any experience is still in Generating status so the UI
  // transitions to Active/Error without manual refresh.
  useEffect(() => {
    const hasGenerating = experiences.some(e => e.status === PUBLIC_EXPERIENCE_STATUS_GENERATING);
    if (!hasGenerating) return;
    const timer = setInterval(() => {
      loadExperiences(true);
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, POLL_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [experiences]);

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
        file_name: fileName,
        raw_content: fileContent,
      });
      closeIngest();
      message.success(t('publicExperience.ingestSuccess'));
      // Refresh list so the newly pre-written Generating record shows up.
      loadExperiences();
    } catch {
      message.error(t('publicExperience.ingestFailed'));
    } finally {
      setIngesting(false);
    }
  };

  const handleRedistill = async (e: React.MouseEvent, id: number) => {
    e.stopPropagation();
    setRedistillingId(id);
    try {
      await publicExperienceApi.redistill(id);
      message.success(t('publicExperience.redistillSuccess'));
      loadExperiences();
    } catch {
      message.error(t('publicExperience.redistillFailed'));
    } finally {
      setRedistillingId(null);
    }
  };

  const sourceLabel = (sourceType: number) =>
    sourceType === 1 ? t('publicExperience.sourceIngestion') : t('publicExperience.sourceShare');

  // Render a status tag for non-Active experiences. Active records show no tag
  // (the normal case) to avoid visual noise.
  const renderStatusTag = (status: number) => {
    if (status === PUBLIC_EXPERIENCE_STATUS_GENERATING) {
      return <Tag color="processing">{t('publicExperience.statusGenerating')}</Tag>;
    }
    if (status === PUBLIC_EXPERIENCE_STATUS_ERROR) {
      return <Tag color="error">{t('publicExperience.statusError')}</Tag>;
    }
    return null;
  };

  // Dynamic title: for non-Active statuses, prepend/append status text around
  // the placeholder title (derived from the uploaded skill). For Active, show
  // the LLM-generated title as-is.
  const displayTitle = (exp: PublicExperience): string => {
    if (exp.status === PUBLIC_EXPERIENCE_STATUS_GENERATING) {
      return t('publicExperience.statusGeneratingTitle', { title: exp.title });
    }
    if (exp.status === PUBLIC_EXPERIENCE_STATUS_ERROR) {
      return t('publicExperience.statusErrorTitle', { title: exp.title });
    }
    return exp.title;
  };

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
            {experiences.map(exp => {
              const isGenerating = exp.status === PUBLIC_EXPERIENCE_STATUS_GENERATING;
              const isError = exp.status === PUBLIC_EXPERIENCE_STATUS_ERROR;
              return (
                <div
                  key={exp.id}
                  className="item-card"
                  style={{ cursor: isGenerating ? 'default' : 'pointer', opacity: isGenerating ? 0.6 : 1 }}
                  onClick={() => !isGenerating && onSelectExp?.(exp)}
                >
                  <div className="item-card-header">
                    <div className="item-card-info" style={{ flex: 1 }}>
                      <div className="item-card-name">
                        {displayTitle(exp)}
                      </div>
                      <div className="item-card-desc">
                        {exp.description}
                      </div>
                      <div className="item-card-desc" style={{ fontSize: '11px', opacity: 0.6, display: 'flex', alignItems: 'center', gap: 6 }}>
                        {renderStatusTag(exp.status)}
                        <span>{sourceLabel(exp.source_type)} · {new Date(exp.created_at).toLocaleDateString()}</span>
                      </div>
                    </div>
                    <div className="item-actions" style={{ display: 'flex', gap: 4 }}>
                      {isError && (
                        <Button
                          type="text"
                          size="small"
                          icon={<ReloadOutlined />}
                          loading={redistillingId === exp.id}
                          onClick={(e) => handleRedistill(e, exp.id)}
                        />
                      )}
                      <Button
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
                </div>
              );
            })}
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

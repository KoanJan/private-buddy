import React, { useEffect, useState } from 'react';
import { Button, Upload, Tag, message, Empty } from 'antd';
import { UploadOutlined, DeleteOutlined, FileTextOutlined, ArrowLeftOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import type { KnowledgeBase, Document } from '../types';
import { DOC_STATUS_FAILED, DOC_STATUS_DELETED } from '../types';
import { kbApi } from '../services/api';
import { logger } from '../logger';
import { confirmDelete } from '../utils/confirm';
import { formatRelativeTime } from '../utils/time';
import { isAllowedFileExtension } from '../constants/fileTypes';

/**
 * Props for the KnowledgeBaseDetail component.
 */
interface KnowledgeBaseDetailProps {
  kb: KnowledgeBase;
  onBack: () => void;
}

/**
 * Document status configuration map.
 * Maps status int values to their display properties (color and i18n key).
 */
const DOC_STATUS_MAP: Record<number, { color: string; labelKey: string }> = {
  0: { color: 'default', labelKey: 'kb.docStatusPending' },
  1: { color: 'processing', labelKey: 'kb.docStatusProcessing' },
  2: { color: 'success', labelKey: 'kb.docStatusReady' },
  3: { color: 'error', labelKey: 'kb.docStatusFailed' },
  4: { color: 'default', labelKey: 'kb.docStatusDeleted' },
};

/**
 * Formats file size from bytes to human-readable string.
 * @param bytes - File size in bytes
 * @returns Formatted string (e.g., "1.5 MB")
 */
function formatFileSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B';
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
}

/**
 * KnowledgeBaseDetail component displays the detail view of a knowledge base.
 * Shows document list, upload functionality, and document management actions.
 */
const KnowledgeBaseDetail: React.FC<KnowledgeBaseDetailProps> = ({ kb, onBack }) => {
  const { t } = useTranslation();
  const [documents, setDocuments] = useState<Document[]>([]);
  const [loading, setLoading] = useState(false);
  const [uploading, setUploading] = useState(false);

  /**
   * Loads the document list for the current knowledge base.
   */
  const loadDocuments = async () => {
    setLoading(true);
    try {
      const response = await kbApi.listDocuments(kb.id);
      setDocuments(response.data);
    } catch (error) {
      logger.error('Failed to load documents:', error);
      message.error(t('messages.loadFailed'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadDocuments();
  }, [kb.id]);

  /**
   * Handles document upload with file type validation.
   * @param file - The file to upload
   * @returns false to prevent default upload behavior
   */
  const handleUpload = async (file: File) => {
    // Validate file extension
    if (!isAllowedFileExtension(file.name)) {
      message.error(t('kb.unsupportedFileType'));
      return false;
    }
    
    setUploading(true);
    try {
      await kbApi.uploadDocument(kb.id, file);
      message.success(t('kb.uploadSuccess'));
      loadDocuments();
    } catch (error) {
      logger.error('Failed to upload document:', error);
      message.error(t('kb.uploadFailed'));
    } finally {
      setUploading(false);
    }
    return false;
  };

  /**
   * Handles document deletion with confirmation dialog.
   * @param docId - The document ID to delete
   */
  const handleDeleteDocument = async (docId: number) => {
    confirmDelete({
      title: t('kb.confirmDeleteDocTitle'),
      content: t('kb.confirmDeleteDoc'),
      okText: t('common.delete'),
      cancelText: t('common.cancel'),
      onOk: async () => {
        try {
          await kbApi.deleteDocument(kb.id, docId);
          message.success(t('kb.deleteDocSuccess'));
          loadDocuments();
        } catch (error) {
          logger.error('Failed to delete document:', error);
          message.error(t('kb.deleteDocFailed'));
        }
      },
    });
  };

  return (
    <div className="kb-detail">
      <div className="kb-detail-header">
        <Button
          type="text"
          icon={<ArrowLeftOutlined />}
          onClick={onBack}
          style={{ color: 'var(--color-text-secondary)' }}
        />
        <div className="kb-detail-title">{kb.name}</div>
      </div>

      <div className="kb-detail-stats">
        <Tag color={DOC_STATUS_MAP[kb.index_type]?.color || 'default'}>
          {t(DOC_STATUS_MAP[kb.index_type]?.labelKey || String(kb.index_type))}
        </Tag>
        <span className="kb-detail-stat">
          {t('kb.docCount', { count: kb.document_count })}
        </span>
        <span className="kb-detail-stat">
          {t('kb.vectorCount', { count: kb.vector_count })}
        </span>
      </div>

      {kb.description && (
        <div className="kb-detail-desc">{kb.description}</div>
      )}

      <div className="kb-detail-upload">
        <Upload
          accept=".pdf,.txt,.md"
          showUploadList={false}
          beforeUpload={(file) => { handleUpload(file); return false; }}
          disabled={uploading}
        >
          <Button icon={<UploadOutlined />} loading={uploading} size="small">
            {t('kb.uploadDocument')}
          </Button>
        </Upload>
        <div className="kb-upload-hint">
          {t('kb.uploadHint')}
        </div>
      </div>

      <div className="kb-detail-docs">
        {loading ? (
          <div className="empty-state-text">{t('sidebar.loading')}</div>
        ) : documents.length === 0 ? (
          <Empty description={t('kb.noDocuments')} image={Empty.PRESENTED_IMAGE_SIMPLE} />
        ) : (
          documents.map(doc => (
            <div key={doc.id} className="kb-doc-item">
              <FileTextOutlined style={{ color: 'var(--color-text-secondary)', fontSize: 16, flexShrink: 0 }} />
              <div className="kb-doc-info">
                <div className="kb-doc-title">{doc.title}</div>
                <div className="kb-doc-meta">
                  <Tag
                    color={DOC_STATUS_MAP[doc.status]?.color || 'default'}
                    style={{ fontSize: 11, lineHeight: '16px', padding: '0 4px' }}
                  >
                    {t(DOC_STATUS_MAP[doc.status]?.labelKey || String(doc.status))}
                  </Tag>
                  {doc.chunk_count > 0 && <span>{doc.chunk_count} chunks</span>}
                  {doc.file_size > 0 && <span>{formatFileSize(doc.file_size)}</span>}
                  <span>{formatRelativeTime(doc.created_at)}</span>
                </div>
                {doc.status === DOC_STATUS_FAILED && doc.error_message && (
                  <div className="kb-doc-error">{doc.error_message}</div>
                )}
              </div>
              {doc.status !== DOC_STATUS_DELETED && (
                <Button
                  type="text"
                  size="small"
                  danger
                  icon={<DeleteOutlined />}
                  onClick={() => handleDeleteDocument(doc.id)}
                  style={{ flexShrink: 0 }}
                />
              )}
            </div>
          ))
        )}
      </div>
    </div>
  );
};

export default KnowledgeBaseDetail;

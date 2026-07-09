import React, { useEffect, useState } from 'react';
import { Spin, Empty } from 'antd';
import { EyeOutlined, FolderOpenOutlined, FileOutlined, DesktopOutlined } from '@ant-design/icons';
import { sessionApi } from '../services/api';
import type { ReceivedDelivery, ReceivedFileEntry } from '../types';

interface ReceivedPanelProps {
  sessionId: number;
}

const formatFileSize = (bytes: number): string => {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
};

const ReceivedPanel: React.FC<ReceivedPanelProps> = ({ sessionId }) => {
  const [deliveries, setDeliveries] = useState<ReceivedDelivery[]>([]);
  const [loading, setLoading] = useState(true);
  const [expandedDelivery, setExpandedDelivery] = useState<string | null>(null);

  const loadDeliveries = async () => {
    setLoading(true);
    try {
      const response = await sessionApi.getReceivedDeliveries(sessionId);
      const data = response.data;
      setDeliveries(data);
      if (data.length > 0) {
        setExpandedDelivery(data[data.length - 1].name);
      }
    } catch {
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadDeliveries();
    // Poll every 10 seconds for new deliveries
    const timer = setInterval(loadDeliveries, 10000);
    return () => clearInterval(timer);
  }, [sessionId]);

  const handleOpen = async (delivery: string, file: ReceivedFileEntry) => {
    if (window.electronAPI) {
      const error = await window.electronAPI.openPath(file.local_path);
      if (error) {
        console.error('[ReceivedPanel] openPath failed:', error);
      }
    } else {
      // In browser, file:// is blocked from http:// origins; serve via backend
      const url = sessionApi.getReceivedFileUrl(sessionId, delivery, file.path);
      window.open(url, '_blank');
    }
  };

  const handleOpenDir = async (files: ReceivedFileEntry[]) => {
    // Derive the delivery directory from the first file's local_path
    const firstPath = files[0]?.local_path;
    if (!firstPath) return;
    const dirPath = firstPath.substring(0, firstPath.lastIndexOf('/'));
    if (window.electronAPI) {
      const error = await window.electronAPI.openPath(dirPath);
      if (error) {
        console.error('[ReceivedPanel] openPath dir failed:', error);
      }
    }
  };

  const toggleDelivery = (name: string) => {
    setExpandedDelivery(expandedDelivery === name ? null : name);
  };

  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: '40px' }}>
        <Spin size="large" />
      </div>
    );
  }

  if (deliveries.length === 0) {
    return (
      <div className="received-panel">
        <Empty description="No delivered files yet" />
      </div>
    );
  }

  return (
    <div className="received-panel">
      <div className="received-header">
        <h3>Delivered Files</h3>
        <span className="received-count">{deliveries.length} delivery{deliveries.length > 1 ? 's' : ''}</span>
      </div>
      <div className="received-list">
        {deliveries.map((delivery) => (
          <div key={delivery.name} className="received-delivery-item">
            <div className={`received-delivery-row ${expandedDelivery === delivery.name ? 'expanded' : ''}`}>
              <button
                className="received-delivery-header"
                onClick={() => toggleDelivery(delivery.name)}
              >
                <FolderOpenOutlined />
                <span className="delivery-name">{delivery.name}</span>
                <span className="delivery-file-count">{delivery.files.length} file{delivery.files.length > 1 ? 's' : ''}</span>
              </button>
              <button
                className="delivery-open-dir-btn"
                onClick={() => handleOpenDir(delivery.files)}
                title="Show in Finder"
              >
                <DesktopOutlined />
              </button>
            </div>
            {expandedDelivery === delivery.name && (
              <div className="received-delivery-files">
                {delivery.files.map((file) => (
                  <div key={file.path} className="received-file-item">
                    <FileOutlined className="file-icon" />
                    <span className="file-name" title={file.path}>{file.name}</span>
                    <span className="file-size">{formatFileSize(file.size)}</span>
                    <button
                      className="file-open-btn"
                      onClick={() => handleOpen(delivery.name, file)}
                      title="Open"
                    >
                      <EyeOutlined />
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
};

export default ReceivedPanel;

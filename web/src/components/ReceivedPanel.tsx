import React, { useEffect, useRef, useState } from 'react';
import { Spin, Empty } from 'antd';
import { CaretRightOutlined, CaretDownOutlined, FolderOpenOutlined, FolderOutlined, FileOutlined, DesktopOutlined, EyeOutlined } from '@ant-design/icons';
import { sessionApi } from '../services/api';
import { logger } from '../logger';
import type { ReceivedDelivery, ReceivedFileEntry } from '../types';
import { formatFileSize } from '../utils/format';

interface ReceivedPanelProps {
  sessionId: number;
}



// Recursive tree node renderer
const FileTreeRenderer: React.FC<{
  nodes: ReceivedFileEntry[];
  delivery: string;
  depth: number;
  onOpenFile: (delivery: string, filePath: string, localPath: string) => void;
}> = ({ nodes, delivery, depth, onOpenFile }) => {
  return (
    <>
      {nodes.map((node) => (
        <TreeNodeItem
          key={node.path}
          node={node}
          delivery={delivery}
          depth={depth}
          onOpenFile={onOpenFile}
        />
      ))}
    </>
  );
};

const TreeNodeItem: React.FC<{
  node: ReceivedFileEntry;
  delivery: string;
  depth: number;
  onOpenFile: (delivery: string, filePath: string, localPath: string) => void;
}> = ({ node, delivery, depth, onOpenFile }) => {
  const [expanded, setExpanded] = useState(true);

  if (node.is_dir) {
    return (
      <div className="received-tree-dir">
        <div
          className="received-tree-item received-tree-dir-row"
          style={{ paddingLeft: depth * 16 }}
          onClick={() => setExpanded(!expanded)}
        >
          {expanded ? <CaretDownOutlined className="tree-caret" /> : <CaretRightOutlined className="tree-caret" />}
          {expanded ? <FolderOpenOutlined className="file-icon" /> : <FolderOutlined className="file-icon" />}
          <span className="file-name">{node.name}/</span>
        </div>
        {expanded && node.children && node.children.length > 0 && (
          <FileTreeRenderer
            nodes={node.children}
            delivery={delivery}
            depth={depth + 1}
            onOpenFile={onOpenFile}
          />
        )}
      </div>
    );
  }

  return (
    <div
      className="received-tree-item received-tree-file-row"
      style={{ paddingLeft: depth * 16 + 20 }}
    >
      <FileOutlined className="file-icon" />
      <span className="file-name" title={node.path}>{node.name}</span>
      {node.size !== undefined && node.size > 0 && (
        <span className="file-size">{formatFileSize(node.size)}</span>
      )}
      <button
        className="file-open-btn"
        onClick={() => {
          if (node.local_path) {
            onOpenFile(delivery, node.path, node.local_path);
          }
        }}
        title={node.local_path ? 'Open' : 'File not available'}
      >
        <EyeOutlined />
      </button>
    </div>
  );
};

const ReceivedPanel: React.FC<ReceivedPanelProps> = ({ sessionId }) => {
  const [deliveries, setDeliveries] = useState<ReceivedDelivery[]>([]);
  const [loading, setLoading] = useState(true);
  const [expandedDelivery, setExpandedDelivery] = useState<string | null>(null);
  const prevDataRef = useRef<string>('');

  const loadDeliveries = async (isPoll: boolean = false) => {
    if (!isPoll) {
      setLoading(true);
    }
    try {
      const response = await sessionApi.getReceivedDeliveries(sessionId);
      const data = response.data;
      const json = JSON.stringify(data);
      // Skip re-render if nothing changed (prevents flicker on poll)
      if (json === prevDataRef.current) {
        return;
      }
      prevDataRef.current = json;
      setDeliveries(data);
      if (data.length > 0) {
        setExpandedDelivery(data[data.length - 1].name);
      }
    } catch (error) {
      logger.error('Failed to load received deliveries', error);
    } finally {
      if (!isPoll) {
        setLoading(false);
      }
    }
  };

  useEffect(() => {
    loadDeliveries(false);
    const timer = setInterval(() => loadDeliveries(true), 10000);
    return () => clearInterval(timer);
  }, [sessionId]);

  const handleOpen = async (delivery: string, filePath: string, localPath: string) => {
    if (window.electronAPI) {
      const error = await window.electronAPI.openPath(localPath);
      if (error) {
        logger.error('[ReceivedPanel] openPath failed:', error);
      }
    } else {
      const url = sessionApi.getReceivedFileUrl(sessionId, delivery, filePath);
      window.open(url, '_blank');
    }
  };

  const handleOpenDir = async (delivery: ReceivedDelivery) => {
    const firstPath = delivery.files[0]?.local_path;
    if (!firstPath) return;
    const dirPath = firstPath.substring(0, firstPath.lastIndexOf('/'));
    if (window.electronAPI) {
      const error = await window.electronAPI.openPath(dirPath);
      if (error) {
        logger.error('[ReceivedPanel] openPath dir failed:', error);
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

  // Count all files in the tree (not just top-level)
  const countTreeFiles = (nodes: ReceivedFileEntry[]): number => {
    let count = 0;
    for (const node of nodes) {
      if (node.is_dir) {
        count += countTreeFiles(node.children);
      } else {
        count++;
      }
    }
    return count;
  };

  return (
    <div className="received-panel">
      <div className="received-list">
        {deliveries.map((delivery) => {
          const totalFiles = countTreeFiles(delivery.files);
          return (
            <div key={delivery.name} className="received-delivery-item">
              <div className={`received-delivery-row ${expandedDelivery === delivery.name ? 'expanded' : ''}`}>
                <button
                  className="received-delivery-header"
                  onClick={() => toggleDelivery(delivery.name)}
                >
                  <FolderOpenOutlined />
                  <span className="delivery-name">{delivery.name}</span>
                  <span className="delivery-file-count">{totalFiles} file{totalFiles > 1 ? 's' : ''}</span>
                </button>
                <button
                  className="delivery-open-dir-btn"
                  onClick={() => handleOpenDir(delivery)}
                  title="Show in Finder"
                >
                  <DesktopOutlined />
                </button>
              </div>
              {expandedDelivery === delivery.name && (
                <div className="received-delivery-files">
                  <FileTreeRenderer
                    nodes={delivery.files}
                    delivery={delivery.name}
                    depth={0}
                    onOpenFile={handleOpen}
                  />
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default ReceivedPanel;

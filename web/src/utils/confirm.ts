import { Modal } from 'antd';

interface ConfirmDeleteOptions {
  title: string;
  content: string;
  okText?: string;
  cancelText?: string;
  onOk: () => Promise<void>;
}

export const confirmDelete = (options: ConfirmDeleteOptions) => {
  Modal.confirm({
    title: options.title,
    content: options.content,
    okText: options.okText || 'Delete',
    okType: 'danger',
    cancelText: options.cancelText || 'Cancel',
    onOk: options.onOk,
  });
};

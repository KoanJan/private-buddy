/**
 * Preload script for Electron context bridge.
 *
 * Exposes a limited, safe API from the main process to the renderer
 * via window.electronAPI. This is the only way the React frontend
 * can communicate with Electron's main process.
 */

import { contextBridge, ipcRenderer } from 'electron';

contextBridge.exposeInMainWorld('electronAPI', {
  getServerPort: (): Promise<number> => ipcRenderer.invoke('get-server-port'),
  getAppVersion: (): Promise<string> => ipcRenderer.invoke('get-app-version'),
  isPackaged: (): Promise<boolean> => ipcRenderer.invoke('is-packaged'),
  getPlatform: (): Promise<string> => ipcRenderer.invoke('get-platform'),
  onBackendStatus: (callback: (status: string) => void) => {
    const handler = (_event: Electron.IpcRendererEvent, status: string) => callback(status);
    ipcRenderer.on('backend-status', handler);
    return () => ipcRenderer.removeListener('backend-status', handler);
  },
  onBackendError: (callback: (error: string) => void) => {
    const handler = (_event: Electron.IpcRendererEvent, error: string) => callback(error);
    ipcRenderer.on('backend-error', handler);
    return () => ipcRenderer.removeListener('backend-error', handler);
  },
});

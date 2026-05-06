/**
 * Electron main process entry point.
 *
 * Manages the application lifecycle:
 * 1. Shows splash screen on app ready
 * 2. Spawns the Go backend server in background
 * 3. Once server health check passes, closes splash and shows main window
 * 4. Handles IPC from renderer (preload bridge)
 * 5. Graceful shutdown of server on quit
 */

import { app, BrowserWindow, ipcMain, globalShortcut } from 'electron';
import path from 'path';
import { startServer, stopServer } from './server-manager';
import { isDev, getWebDistPath, getServerPort, APP_NAME } from './config';

let mainWindow: BrowserWindow | null = null;
let splashWindow: BrowserWindow | null = null;

function createSplashWindow(): BrowserWindow {
  splashWindow = new BrowserWindow({
    width: 400,
    height: 280,
    frame: false,
    resizable: false,
    transparent: true,
    center: true,
    show: false,
    webPreferences: {
      nodeIntegration: false,
      contextIsolation: true,
    },
  });

  const splashPath = path.join(__dirname, 'splash.html');
  splashWindow.loadFile(splashPath);

  splashWindow.once('ready-to-show', () => {
    splashWindow?.show();
  });

  splashWindow.on('closed', () => {
    splashWindow = null;
  });

  return splashWindow;
}

function createMainWindow(autoShow: boolean = false): void {
  const isMac = process.platform === 'darwin';

  mainWindow = new BrowserWindow({
    width: 1200,
    height: 800,
    minWidth: 800,
    minHeight: 600,
    title: APP_NAME,
    titleBarStyle: 'hidden',
    titleBarOverlay: {
      height: 38,
      color: '#ffffff',
      symbolColor: '#6b7280',
    },
    show: false,
    webPreferences: {
      nodeIntegration: false,
      contextIsolation: true,
      preload: path.join(__dirname, 'preload.js'),
    },
  });

  const webPath = getWebDistPath();
  if (isDev()) {
    mainWindow.loadURL(webPath);
  } else {
    mainWindow.loadFile(webPath);
  }

  if (autoShow) {
    mainWindow.once('ready-to-show', () => {
      mainWindow?.show();
    });
  }

  mainWindow.on('closed', () => {
    mainWindow = null;
  });

  globalShortcut.register('CommandOrControl+Shift+I', () => {
    mainWindow?.webContents.toggleDevTools();
  });
}

function showMainWindow(): void {
  if (!mainWindow || mainWindow.isDestroyed()) return;
  if (splashWindow && !splashWindow.isDestroyed()) {
    splashWindow.close();
  }
  mainWindow.show();
}

app.on('ready', async () => {
  ipcMain.handle('get-server-port', () => {
    const port = getServerPort();
    console.log('[IPC] get-server-port called, returning:', port);
    return port;
  });
  ipcMain.handle('get-app-version', () => app.getVersion());
  ipcMain.handle('is-packaged', () => app.isPackaged);
  ipcMain.handle('get-platform', () => process.platform);

  createSplashWindow();

  createMainWindow();

  startServer()
    .then(() => {
      console.log('Server started successfully');
      mainWindow?.webContents.send('backend-status', 'ready');
    })
    .catch((err: Error) => {
      const errMsg = err.message;
      console.error('Failed to start server:', errMsg);
      mainWindow?.webContents.send('backend-error', errMsg);
    });

  mainWindow?.webContents.once('did-finish-load', () => {
    mainWindow?.webContents.send('backend-status', 'ready');
    showMainWindow();
  });
});

app.on('window-all-closed', () => {
  stopServer();
  app.quit();
});

app.on('before-quit', () => {
  globalShortcut.unregisterAll();
  stopServer();
});

app.on('activate', () => {
  if (mainWindow === null) {
    createMainWindow(true);
  }
});

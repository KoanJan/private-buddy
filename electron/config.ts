/**
 * Application configuration constants for Electron main process.
 *
 * Centralizes paths, ports, and environment detection used by
 * the main process and python manager.
 */

import { app } from 'electron';
import path from 'path';
import net from 'net';

export const APP_NAME = 'Private Buddy';

export const SERVER_HOST = '127.0.0.1';

let assignedPort: number | null = null;

export async function findFreePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.listen(0, SERVER_HOST, () => {
      const addr = server.address();
      if (addr && typeof addr !== 'string' && addr.port) {
        const port = addr.port;
        server.close(() => resolve(port));
      } else {
        server.close();
        reject(new Error('Failed to get address from server'));
      }
    });
    server.on('error', (err) => {
      reject(err);
    });
  });
}

export function getServerPort(): number {
  return assignedPort ?? 8000;
}

export function setServerPort(port: number): void {
  assignedPort = port;
}

export function isDev(): boolean {
  return !app.isPackaged;
}

export function getProjectRoot(): string {
  if (isDev()) {
    return path.resolve(__dirname, '..', '..');
  }
  return path.dirname(app.getPath('exe'));
}

export function getPythonExecutable(): string {
  if (isDev()) {
    return path.join(getProjectRoot(), 'server', 'venv', 'bin', 'python');
  }
  return path.join(process.resourcesPath, 'python-server', 'private-buddy-server', 'private-buddy-server');
}

export function getServerCwd(): string {
  if (isDev()) {
    return path.join(getProjectRoot(), 'server');
  }
  return path.join(process.resourcesPath, 'python-server', 'private-buddy-server');
}

export function getWebDistPath(): string {
  if (isDev()) {
    return `http://localhost:5173`;
  }
  return path.resolve(__dirname, '..', '..', 'web-dist', 'index.html');
}

export function getServerUrl(): string {
  return `http://${SERVER_HOST}:${getServerPort()}`;
}

export function getDataRoot(): string {
  return path.join(app.getPath('userData'), 'data');
}

/**
 * Python server process lifecycle manager.
 *
 * Handles spawning the Python backend, waiting for it to become
 * ready (health check polling), and graceful shutdown on app quit.
 *
 * In dev mode: spawns `python -m uvicorn app.main:app`
 * In production: spawns the PyInstaller-bundled executable directly
 */

import { ChildProcess, spawn } from 'child_process';
import { getPythonExecutable, getServerCwd, isDev, SERVER_HOST, getServerPort, setServerPort, findFreePort, getDataRoot } from './config';
import { app } from 'electron';
import http from 'http';

let pythonProcess: ChildProcess | null = null;

function healthCheck(host: string, port: number, maxRetries: number = 30, intervalMs: number = 1000): Promise<void> {
  return new Promise((resolve, reject) => {
    let attempts = 0;

    const check = () => {
      attempts++;
      const req = http.get(
        `http://${host}:${port}/`,
        { timeout: 2000 },
        (res) => {
          if (res.statusCode === 200) {
            resolve();
          } else if (attempts < maxRetries) {
            setTimeout(check, intervalMs);
          } else {
            reject(new Error(`Server returned status ${res.statusCode} after ${maxRetries} attempts`));
          }
        }
      );

      req.on('error', () => {
        if (attempts < maxRetries) {
          setTimeout(check, intervalMs);
        } else {
          reject(new Error(`Server not ready after ${maxRetries} attempts`));
        }
      });

      req.on('timeout', () => {
        req.destroy();
        if (attempts < maxRetries) {
          setTimeout(check, intervalMs);
        } else {
          reject(new Error(`Server health check timed out after ${maxRetries} attempts`));
        }
      });
    };

    check();
  });
}

const MAX_PORT_RETRIES = 5;

export async function startPythonServer(): Promise<void> {
  for (let attempt = 0; attempt < MAX_PORT_RETRIES; attempt++) {
    const port = await findFreePort();
    setServerPort(port);

    const exe = getPythonExecutable();
    const cwd = getServerCwd();

    console.log(`[Python] Spawning server (attempt ${attempt + 1}): exe=${exe}, cwd=${cwd}, dev=${isDev()}, port=${port}`);

    const args = isDev()
      ? ['-m', 'uvicorn', 'app.main:app', '--host', SERVER_HOST, '--port', String(port)]
      : ['--host', SERVER_HOST, '--port', String(port)];

    const env = {
      ...process.env,
      PORT: String(port),
      DATA_ROOT: getDataRoot(),
    };

    pythonProcess = spawn(exe, args, {
      cwd,
      env,
      stdio: ['ignore', 'pipe', 'pipe'],
    });

    pythonProcess.stdout?.on('data', (data: Buffer) => {
      console.log(`[Python] ${data.toString().trim()}`);
    });

    pythonProcess.stderr?.on('data', (data: Buffer) => {
      console.error(`[Python] ${data.toString().trim()}`);
    });

    pythonProcess.on('error', (err) => {
      console.error('Failed to start Python server:', err);
    });

    pythonProcess.on('exit', (code, signal) => {
      if (code !== null && code !== 0 && code !== 1) {
        console.error(`Python server exited with code ${code}, signal ${signal}`);
      }
      pythonProcess = null;
    });

    try {
      await healthCheck(SERVER_HOST, port);
      console.log(`Python server is ready on port ${port}`);
      return;
    } catch (err) {
      console.warn(`[Python] Health check failed on port ${port}:`, err);
      stopPythonServer();
      if (attempt < MAX_PORT_RETRIES - 1) {
        console.log(`[Python] Retrying with a new port...`);
      } else {
        throw new Error(`Failed to start Python server after ${MAX_PORT_RETRIES} attempts`);
      }
    }
  }
}

export function stopPythonServer(): void {
  if (!pythonProcess) {
    return;
  }

  if (process.platform === 'win32') {
    pythonProcess.kill();
  } else {
    pythonProcess.kill('SIGTERM');
  }

  const forceKillTimer = setTimeout(() => {
    if (pythonProcess) {
      pythonProcess.kill('SIGKILL');
      pythonProcess = null;
    }
  }, 5000);

  pythonProcess.on('exit', () => {
    clearTimeout(forceKillTimer);
    pythonProcess = null;
  });
}

export function isServerRunning(): boolean {
  return pythonProcess !== null;
}

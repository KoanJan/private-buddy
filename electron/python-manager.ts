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
import { existsSync } from 'fs';

let pythonProcess: ChildProcess | null = null;

function healthCheck(host: string, port: number, maxRetries: number = 15, intervalMs: number = 1000): Promise<void> {
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

const MAX_PORT_RETRIES = 3;

export async function startPythonServer(): Promise<void> {
  for (let attempt = 0; attempt < MAX_PORT_RETRIES; attempt++) {
    const port = await findFreePort();
    setServerPort(port);

    const exe = getPythonExecutable();
    const cwd = getServerCwd();

    console.log(`[Python] Spawning server (attempt ${attempt + 1}/${MAX_PORT_RETRIES}):`);
    console.log(`[Python]   exe=${exe}`);
    console.log(`[Python]   cwd=${cwd}`);
    console.log(`[Python]   dev=${isDev()}, port=${port}, platform=${process.platform}`);

    if (!existsSync(exe)) {
      throw new Error(`Python executable not found: ${exe}. Ensure PyInstaller build was run before packaging.`);
    }

    const args = isDev()
      ? ['-m', 'uvicorn', 'app.main:app', '--host', SERVER_HOST, '--port', String(port)]
      : ['--host', SERVER_HOST, '--port', String(port)];

    console.log(`[Python]   args=${args.join(' ')}`);

    const env = {
      ...process.env,
      PORT: String(port),
      DATA_ROOT: getDataRoot(),
    };

    let spawnError: Error | null = null;

    pythonProcess = spawn(exe, args, {
      cwd,
      env,
      stdio: ['ignore', 'pipe', 'pipe'],
    });

    pythonProcess.stdout?.on('data', (data: Buffer) => {
      console.log(`[Python] stdout: ${data.toString().trim()}`);
    });

    pythonProcess.stderr?.on('data', (data: Buffer) => {
      console.error(`[Python] stderr: ${data.toString().trim()}`);
    });

    pythonProcess.on('error', (err) => {
      spawnError = err;
      console.error(`[Python] spawn error: ${err.message} (code=${(err as NodeJS.ErrnoException).code})`);
    });

    pythonProcess.on('exit', (code, signal) => {
      if (code !== null && code !== 0) {
        console.error(`[Python] exited with code ${code}, signal ${signal}`);
      }
      pythonProcess = null;
    });

    try {
      await healthCheck(SERVER_HOST, port);
      console.log(`Python server is ready on port ${port}`);
      return;
    } catch (err) {
      const cause = spawnError || err;
      console.warn(`[Python] Health check failed on port ${port}:`, cause);
      stopPythonServer();
      if (attempt < MAX_PORT_RETRIES - 1) {
        console.log(`[Python] Retrying with a new port...`);
      } else {
        throw new Error(`Failed to start Python server after ${MAX_PORT_RETRIES} attempts. Last error: ${cause instanceof Error ? cause.message : String(cause)}`);
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
    pythonProcess = null;
  } else {
    pythonProcess.kill('SIGTERM');

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
}

export function isServerRunning(): boolean {
  return pythonProcess !== null;
}

/**
 * Go server process lifecycle manager.
 *
 * Handles spawning the Go backend, waiting for it to become
 * ready (health check polling), and graceful shutdown on app quit.
 *
 * In dev mode: spawns the pre-built Go binary from server_go/
 * In production: spawns the bundled Go binary from resources
 */

import { ChildProcess, spawn } from 'child_process';
import { getGoServerExecutable, getGoServerCwd, isDev, SERVER_HOST, getServerPort, setServerPort, findFreePort, getDataRoot } from './config';
import http from 'http';
import { existsSync } from 'fs';

let goProcess: ChildProcess | null = null;

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

export async function startGoServer(): Promise<void> {
  for (let attempt = 0; attempt < MAX_PORT_RETRIES; attempt++) {
    const port = await findFreePort();
    setServerPort(port);

    const exe = getGoServerExecutable();
    const cwd = getGoServerCwd();

    console.log(`[GoServer] Spawning server (attempt ${attempt + 1}/${MAX_PORT_RETRIES}):`);
    console.log(`[GoServer]   exe=${exe}`);
    console.log(`[GoServer]   cwd=${cwd}`);
    console.log(`[GoServer]   dev=${isDev()}, port=${port}, platform=${process.platform}`);

    if (!existsSync(exe)) {
      throw new Error(`Go server executable not found: ${exe}. Run: cd server_go && go build -o private-buddy-server ./cmd/server/`);
    }

    const args: string[] = [];

    console.log(`[GoServer]   args=${args.join(' ') || '(none)'}`);

    const env = {
      ...process.env,
      PORT: String(port),
      DATA_ROOT: getDataRoot(),
    };

    let spawnError: Error | null = null;

    goProcess = spawn(exe, args, {
      cwd,
      env,
      stdio: ['ignore', 'pipe', 'pipe'],
    });

    goProcess.stdout?.on('data', (data: Buffer) => {
      console.log(`[GoServer] stdout: ${data.toString().trim()}`);
    });

    goProcess.stderr?.on('data', (data: Buffer) => {
      console.error(`[GoServer] stderr: ${data.toString().trim()}`);
    });

    goProcess.on('error', (err) => {
      spawnError = err;
      console.error(`[GoServer] spawn error: ${err.message} (code=${(err as NodeJS.ErrnoException).code})`);
    });

    goProcess.on('exit', (code, signal) => {
      if (code !== null && code !== 0) {
        console.error(`[GoServer] exited with code ${code}, signal ${signal}`);
      }
      goProcess = null;
    });

    try {
      await healthCheck(SERVER_HOST, port);
      console.log(`Go server is ready on port ${port}`);
      return;
    } catch (err) {
      const cause = spawnError || err;
      console.warn(`[GoServer] Health check failed on port ${port}:`, cause);
      stopGoServer();
      if (attempt < MAX_PORT_RETRIES - 1) {
        console.log(`[GoServer] Retrying with a new port...`);
      } else {
        throw new Error(`Failed to start Go server after ${MAX_PORT_RETRIES} attempts. Last error: ${cause instanceof Error ? cause.message : String(cause)}`);
      }
    }
  }
}

export function stopGoServer(): void {
  if (!goProcess) {
    return;
  }

  if (process.platform === 'win32') {
    goProcess.kill();
    goProcess = null;
  } else {
    goProcess.kill('SIGTERM');

    const forceKillTimer = setTimeout(() => {
      if (goProcess) {
        goProcess.kill('SIGKILL');
        goProcess = null;
      }
    }, 5000);

    goProcess.on('exit', () => {
      clearTimeout(forceKillTimer);
      goProcess = null;
    });
  }
}

export function isServerRunning(): boolean {
  return goProcess !== null;
}

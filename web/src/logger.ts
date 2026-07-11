import { storage } from './services/storage';

const LOG_KEY = 'private_buddy_logs';
const MAX_LOGS = 1000;

const getTimestamp = () => {
  return new Date().toISOString();
};

const saveLog = (level: string, ...args: unknown[]): string => {
  const timestamp = getTimestamp();
  const message = args.map(arg => {
    if (arg instanceof Error) {
      return `${arg.name}: ${arg.message}\n${arg.stack}`;
    }
    if (typeof arg === 'object') {
      try {
        return JSON.stringify(arg, null, 2);
      } catch {
        return String(arg);
      }
    }
    return String(arg);
  }).join(' ');
  
  const logEntry = `[${timestamp}] [${level}] ${message}`;
  
  try {
    const logs: string[] = storage.get<string[]>(LOG_KEY) || [];
    logs.push(logEntry);
    
    if (logs.length > MAX_LOGS) {
      logs.splice(0, logs.length - MAX_LOGS);
    }
    
    storage.set(LOG_KEY, logs);
  } catch (error) {
    console.error('Failed to save log:', error);
  }
  
  return logEntry;
};

/** Application logger with in-memory persistence and download support. */
export const logger = {
  info: (...args: unknown[]): void => {
    const logEntry = saveLog('INFO', ...args);
    console.log(`%c${logEntry}`, 'color: #2196F3');
  },
  
  error: (...args: unknown[]): void => {
    const logEntry = saveLog('ERROR', ...args);
    console.error(logEntry);
  },
  
  warn: (...args: unknown[]): void => {
    const logEntry = saveLog('WARN', ...args);
    console.warn(logEntry);
  },
  
  debug: (...args: unknown[]): void => {
    const logEntry = saveLog('DEBUG', ...args);
    console.log(`%c${logEntry}`, 'color: #9E9E9E');
  },
  
  getLogs: () => {
    return storage.get<string[]>(LOG_KEY) || [];
  },
  
  clearLogs: () => {
    storage.remove(LOG_KEY);
    console.log('Logs cleared');
  },
  
  downloadLogs: () => {
    const logs = logger.getLogs();
    const content = logs.join('\n');
    const blob = new Blob([content], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `private_buddy_logs_${new Date().toISOString().split('T')[0]}.txt`;
    a.click();
    URL.revokeObjectURL(url);
  }
};

(window as { logger?: typeof logger }).logger = logger;

console.log('Logger initialized. Use window.logger.getLogs() to view logs, window.logger.downloadLogs() to download logs.');
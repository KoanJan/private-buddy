import i18n from '../i18n';

/**
 * Format a message timestamp for display.
 *
 * - Same day: time only (e.g. "14:30:00")
 * - Yesterday: "Yesterday" + time (e.g. "Yesterday 14:30:00")
 * - Older: full date + time (e.g. "2025-04-30 14:30:00")
 */
export function formatMessageTime(date: Date): string {
  const now = new Date();
  const timeStr = date.toLocaleTimeString(i18n.language, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });

  const isSameDay =
    date.getFullYear() === now.getFullYear() &&
    date.getMonth() === now.getMonth() &&
    date.getDate() === now.getDate();

  if (isSameDay) {
    return timeStr;
  }

  const yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);
  const isYesterday =
    date.getFullYear() === yesterday.getFullYear() &&
    date.getMonth() === yesterday.getMonth() &&
    date.getDate() === yesterday.getDate();

  if (isYesterday) {
    const yesterdayLabel = i18n.t('chat.yesterday');
    return `${yesterdayLabel} ${timeStr}`;
  }

  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  return `${year}-${month}-${day} ${timeStr}`;
}

/**
 * Format an ISO timestamp as a relative time string.
 * Returns a human-readable approximation like "2m ago", "1h ago", "3d ago".
 */
export function formatRelativeTime(isoStr: string): string {
  const date = new Date(isoStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffSec = Math.floor(diffMs / 1000);

  if (diffSec < 60) return 'just now';
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHour = Math.floor(diffMin / 60);
  if (diffHour < 24) return `${diffHour}h ago`;
  const diffDay = Math.floor(diffHour / 24);
  if (diffDay < 30) return `${diffDay}d ago`;
  const diffMonth = Math.floor(diffDay / 30);
  if (diffMonth < 12) return `${diffMonth}mo ago`;
  const diffYear = Math.floor(diffMonth / 12);
  return `${diffYear}y ago`;
}

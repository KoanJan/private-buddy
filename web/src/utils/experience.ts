import type { TFunction } from 'i18next';
import type { PublicExperience } from '../types';
import { PUBLIC_EXPERIENCE_STATUS_GENERATING, PUBLIC_EXPERIENCE_STATUS_ERROR } from '../types';

/**
 * Returns a human-readable label for the experience source type.
 */
export function experienceSourceLabel(sourceType: number, t: TFunction): string {
  return sourceType === 1
    ? t('publicExperience.sourceIngestion')
    : t('publicExperience.sourceShare');
}

/**
 * Returns { color, label } for the status tag, or null for Active (normal case).
 * Caller renders: {info && <Tag color={info.color}>{info.label}</Tag>}
 */
export function experienceStatusInfo(
  status: number,
  t: TFunction,
): { color: string; label: string } | null {
  if (status === PUBLIC_EXPERIENCE_STATUS_GENERATING) {
    return { color: 'processing', label: t('publicExperience.statusGenerating') };
  }
  if (status === PUBLIC_EXPERIENCE_STATUS_ERROR) {
    return { color: 'error', label: t('publicExperience.statusError') };
  }
  return null;
}

/**
 * Returns the display title for an experience, decorating non-Active
 * statuses with context (e.g., "Extracting ..." for Generating status).
 */
export function experienceDisplayTitle(exp: PublicExperience, t: TFunction): string {
  if (exp.status === PUBLIC_EXPERIENCE_STATUS_GENERATING) {
    return t('publicExperience.statusGeneratingTitle', { title: exp.title });
  }
  if (exp.status === PUBLIC_EXPERIENCE_STATUS_ERROR) {
    return t('publicExperience.statusErrorTitle', { title: exp.title });
  }
  return exp.title;
}

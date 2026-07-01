import React, { useEffect, useState } from 'react';
import { Segmented, Spin } from 'antd';
import { useTranslation } from 'react-i18next';
import type { PublicExperience } from '../types';
import { uploadedSkillApi } from '../services/api';

interface PublicExperienceDetailProps {
  exp: PublicExperience;
}

const PublicExperienceDetail: React.FC<PublicExperienceDetailProps> = ({ exp }) => {
  const { t } = useTranslation();
  const [tab, setTab] = useState<'content' | 'source'>('content');
  const [sourceContent, setSourceContent] = useState<string | null>(null);
  const [sourceLoading, setSourceLoading] = useState(false);

  const hasSource = exp.source_type === 1 && exp.source_id > 0;

  // Lazy-load source content only when the source tab is first selected.
  useEffect(() => {
    if (tab !== 'source' || sourceContent !== null || sourceLoading) return;
    if (!hasSource) return;
    setSourceLoading(true);
    uploadedSkillApi.get(exp.source_id)
      .then(res => setSourceContent(res.data.raw_content))
      .catch(() => setSourceContent(null))
      .finally(() => setSourceLoading(false));
  }, [tab, hasSource, exp, sourceContent, sourceLoading]);

  const renderField = (label: string, content: string) => {
    if (!content) return null;
    return (
      <div style={{ marginBottom: 20 }}>
        <div style={{ fontWeight: 600, marginBottom: 6, color: 'var(--color-text-primary)' }}>{label}</div>
        <div style={{ whiteSpace: 'pre-wrap', lineHeight: 1.8, color: 'var(--color-text-secondary)' }}>{content}</div>
      </div>
    );
  };

  const sourceLabel = exp.source_type === 1
    ? t('publicExperience.sourceIngestion')
    : t('publicExperience.sourceShare');
  const formattedDate = new Date(exp.created_at).toLocaleString('sv-SE').replace('T', ' ');

  return (
    <div>
      <h3 style={{ fontSize: 18, fontWeight: 600, marginBottom: 16, color: 'var(--color-text-primary)' }}>
        {exp.title}
      </h3>
      <div style={{ fontSize: 12, color: 'var(--color-text-placeholder)', marginBottom: 16 }}>
        {sourceLabel} · {formattedDate}
      </div>

      {hasSource && (
        <div style={{ marginBottom: 20 }}>
          <Segmented
            size="small"
            value={tab}
            onChange={(val) => setTab(val as 'content' | 'source')}
            options={[
              { label: 'Content', value: 'content' },
              { label: 'source SKILL.md', value: 'source' },
            ]}
          />
        </div>
      )}

      {tab === 'content' ? (
        <>
          <p style={{ color: 'var(--color-text-secondary)', marginBottom: 24, lineHeight: 1.8 }}>
            {exp.description}
          </p>

          {renderField(t('publicExperience.fieldWhenToUse'), exp.when_to_use)}
          {renderField(t('publicExperience.fieldGuidelines'), exp.guidelines)}
          {renderField(t('publicExperience.fieldPitfalls'), exp.pitfalls)}
          {renderField(t('publicExperience.fieldProcedure'), exp.procedure)}
        </>
      ) : sourceLoading ? (
        <div style={{ display: 'flex', justifyContent: 'center', padding: 48 }}>
          <Spin />
        </div>
      ) : sourceContent ? (
        <pre style={{
          margin: 0,
          padding: 16,
          fontFamily: 'monospace',
          fontSize: 12,
          lineHeight: 1.6,
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
          background: 'var(--color-bg-secondary)',
          borderRadius: 8,
        }}>
          {sourceContent}
        </pre>
      ) : (
        <div style={{ color: 'var(--color-text-secondary)', padding: 24, textAlign: 'center' }}>
          {t('publicExperience.sourceContentUnavailable')}
        </div>
      )}
    </div>
  );
};

export default PublicExperienceDetail;

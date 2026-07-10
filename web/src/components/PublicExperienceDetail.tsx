import React, { useEffect, useState } from 'react';
import { Segmented, Spin, Tag, Button, message } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import type { PublicExperience } from '../types';
import {
  PUBLIC_EXPERIENCE_STATUS_GENERATING,
  PUBLIC_EXPERIENCE_STATUS_ERROR,
} from '../types';
import { experienceSourceLabel, experienceStatusInfo, experienceDisplayTitle } from '../utils/experience';
import { uploadedSkillApi, publicExperienceApi } from '../services/api';

interface PublicExperienceDetailProps {
  exp: PublicExperience;
  // Called after a successful re-distill request so the parent can navigate
  // back to the list (which will remount and show the Generating status).
  onRedistilled?: () => void;
}

const PublicExperienceDetail: React.FC<PublicExperienceDetailProps> = ({ exp, onRedistilled }) => {
  const { t } = useTranslation();
  const [tab, setTab] = useState<'content' | 'source'>('content');
  const [sourceContent, setSourceContent] = useState<string | null>(null);
  const [sourceLoading, setSourceLoading] = useState(false);
  const [redistilling, setRedistilling] = useState(false);

  const hasSource = exp.source_type === 1 && exp.source_id > 0;
  const isGenerating = exp.status === PUBLIC_EXPERIENCE_STATUS_GENERATING;
  const isError = exp.status === PUBLIC_EXPERIENCE_STATUS_ERROR;

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

  const handleRedistill = async () => {
    setRedistilling(true);
    try {
      await publicExperienceApi.redistill(exp.id);
      message.success(t('publicExperience.redistillSuccess'));
      onRedistilled?.();
    } catch {
      message.error(t('publicExperience.redistillFailed'));
    } finally {
      setRedistilling(false);
    }
  };

  const sourceLabel = experienceSourceLabel(exp.source_type, t);
  const formattedDate = new Date(exp.created_at).toLocaleString('sv-SE').replace('T', ' ');

  const renderStatusTag = () => {
    const info = experienceStatusInfo(exp.status, t);
    if (!info) return null;
    return <Tag color={info.color}>{info.label}</Tag>;
  };

  // Dynamic title: for non-Active statuses, prepend/append status text around
  // the placeholder title. For Active, show the LLM-generated title as-is.
  const displayTitle = () => experienceDisplayTitle(exp, t);

  return (
    <div>
      <h3 style={{ fontSize: 18, fontWeight: 600, marginBottom: 16, color: 'var(--color-text-primary)' }}>
        {displayTitle()}
      </h3>
      <div style={{ fontSize: 12, color: 'var(--color-text-placeholder)', marginBottom: 16, display: 'flex', alignItems: 'center', gap: 6 }}>
        {renderStatusTag()}
        <span>{sourceLabel} · {formattedDate}</span>
      </div>

      {isError && (
        <div style={{ marginBottom: 20 }}>
          <Button
            type="primary"
            icon={<ReloadOutlined />}
            loading={redistilling}
            onClick={handleRedistill}
          >
            {t('publicExperience.redistill')}
          </Button>
        </div>
      )}

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

      {isGenerating ? (
        // While the LLM is distilling, content fields are empty — show a
        // spinner instead of a blank panel.
        <div style={{ display: 'flex', justifyContent: 'center', padding: 48 }}>
          <Spin />
        </div>
      ) : tab === 'content' ? (
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

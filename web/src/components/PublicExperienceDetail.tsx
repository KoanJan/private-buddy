import React, { useEffect, useState } from 'react';
import { Segmented, Spin, Tag, Button, message } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import { MarkdownRenderer } from 'pd-markdown/web';
import type { PublicExperience } from '../types';
import {
  PUBLIC_EXPERIENCE_STATUS_GENERATING,
  PUBLIC_EXPERIENCE_STATUS_ERROR,
} from '../types';
import { experienceSourceLabel, experienceStatusInfo, experienceDisplayTitle } from '../utils/experience';
import { uploadedSkillApi, publicExperienceApi } from '../services/api';

interface PublicExperienceDetailProps {
  exp: PublicExperience;
  onRedistilled?: () => void;
}

// Split raw SKILL.md content into frontmatter (key-value table) and body.
// pd-markdown parses frontmatter as metadata but does not render it,
// so we extract and display it separately.
const splitFrontmatter = (raw: string): { fm: Record<string, string> | null; body: string } => {
  const lines = raw.split('\n');
  if (lines[0]?.trim() !== '---') return { fm: null, body: raw };

  const endIdx = lines.findIndex((l, i) => i > 0 && l.trim() === '---');
  if (endIdx === -1) return { fm: null, body: raw };

  const fm: Record<string, string> = {};
  for (let i = 1; i < endIdx; i++) {
    const colonIdx = lines[i].indexOf(':');
    if (colonIdx > 0) {
      fm[lines[i].slice(0, colonIdx).trim()] = lines[i].slice(colonIdx + 1).trim();
    }
  }
  const body = lines.slice(endIdx + 1).join('\n');
  return { fm: Object.keys(fm).length > 0 ? fm : null, body };
};

const renderFrontmatterTable = (fm: Record<string, string>) => {
  const entries = Object.entries(fm);
  if (entries.length === 0) return null;
  return (
    <table style={{
      width: '100%',
      borderCollapse: 'collapse',
      marginBottom: 20,
      fontSize: 13,
    }}>
      <tbody>
        {entries.map(([key, value]) => (
          <tr key={key} style={{ borderBottom: '1px solid var(--color-border, #e5e7eb)' }}>
            <td style={{
              padding: '6px 12px',
              fontWeight: 600,
              color: 'var(--color-text-primary)',
              whiteSpace: 'nowrap',
              width: '1%',
            }}>{key}</td>
            <td style={{
              padding: '6px 12px',
              color: 'var(--color-text-secondary)',
            }}>{value}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
};

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
        <div className="message-content" style={{ maxWidth: 'none', borderRadius: 0, backgroundColor: 'transparent' }}>
          <MarkdownRenderer source={content} />
        </div>
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

  const renderSourceContent = () => {
    if (sourceLoading) {
      return (
        <div style={{ display: 'flex', justifyContent: 'center', padding: 48 }}>
          <Spin />
        </div>
      );
    }
    if (!sourceContent) {
      return (
        <div style={{ color: 'var(--color-text-secondary)', padding: 24, textAlign: 'center' }}>
          {t('publicExperience.sourceContentUnavailable')}
        </div>
      );
    }

    const { fm, body } = splitFrontmatter(sourceContent);
    return (
      <div className="message-content" style={{ maxWidth: 'none', borderRadius: 0, backgroundColor: 'transparent' }}>
        {fm && renderFrontmatterTable(fm)}
        <MarkdownRenderer source={body} />
      </div>
    );
  };

  return (
    <div>
      <h3 style={{ fontSize: 18, fontWeight: 600, marginBottom: 16, color: 'var(--color-text-primary)' }}>
        {experienceDisplayTitle(exp, t)}
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
        <div style={{ display: 'flex', justifyContent: 'center', padding: 48 }}>
          <Spin />
        </div>
      ) : tab === 'content' ? (
        <>
          <div className="message-content" style={{ maxWidth: 'none', borderRadius: 0, backgroundColor: 'transparent' }}>
            <MarkdownRenderer source={exp.description} />
          </div>

          {renderField(t('publicExperience.fieldWhenToUse'), exp.when_to_use)}
          {renderField(t('publicExperience.fieldGuidelines'), exp.guidelines)}
          {renderField(t('publicExperience.fieldPitfalls'), exp.pitfalls)}
          {renderField(t('publicExperience.fieldProcedure'), exp.procedure)}
        </>
      ) : (
        renderSourceContent()
      )}
    </div>
  );
};

export default PublicExperienceDetail;
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { cloudApi } from '../api/cloud';
import { getPlatform } from '../platform';

export function SubmitTicket() {
  const { t } = useTranslation('feedback');
  const navigate = useNavigate();
  const [subject, setSubject] = useState('');
  const [content, setContent] = useState('');
  const [shouldUploadLogs, setShouldUploadLogs] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const handleUploadLogs = () => {
    setShouldUploadLogs(true);
  };

  const handleSubmit = async () => {
    if (!subject.trim() || !content.trim()) return;
    setSubmitting(true);
    try {
      const resp = await cloudApi.createIssue(subject, content);
      const data = resp.data as { id: string } | undefined;

      if (shouldUploadLogs && data?.id) {
        const platform = getPlatform();
        await platform.uploadLogs(data.id);
      }

      navigate('/issues');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="p-4 space-y-6">
      <h1 className="text-xl font-semibold">{t('submitTicketTitle')}</h1>

      <div className="space-y-4">
        {/* Subject field */}
        <div>
          <label className="block text-sm font-medium text-[--color-text-secondary] mb-1">
            {t('subject')}
          </label>
          <input
            type="text"
            value={subject}
            onChange={(e) => setSubject(e.target.value)}
            placeholder={t('subjectPlaceholder')}
            className="w-full rounded-lg p-3 bg-[--color-bg-paper] text-[--color-text-primary] border border-[--color-text-disabled]/30 placeholder:text-[--color-text-disabled]"
          />
        </div>

        {/* Content field */}
        <div>
          <label className="block text-sm font-medium text-[--color-text-secondary] mb-1">
            {t('content')}
          </label>
          <textarea
            value={content}
            onChange={(e) => setContent(e.target.value)}
            placeholder={t('contentPlaceholder')}
            className="w-full rounded-lg p-3 bg-[--color-bg-paper] text-[--color-text-primary] border border-[--color-text-disabled]/30 resize-none min-h-[120px] placeholder:text-[--color-text-disabled]"
          />
        </div>

        {/* Upload logs button */}
        <button
          type="button"
          onClick={handleUploadLogs}
          className={`py-2 px-4 rounded-lg border transition-colors ${
            shouldUploadLogs
              ? 'border-[--color-success] text-[--color-success] bg-[--color-success]/10'
              : 'border-[--color-text-disabled]/30 text-[--color-text-secondary] hover:border-[--color-primary] hover:text-[--color-primary]'
          }`}
        >
          {t('uploadLogs')}
        </button>
      </div>

      {/* Submit button */}
      <button
        onClick={handleSubmit}
        disabled={submitting || !subject.trim() || !content.trim()}
        className="w-full py-3 rounded-lg bg-[--color-primary] text-white font-medium hover:opacity-90 transition-opacity disabled:opacity-50"
      >
        {submitting ? t('submitting') : t('submit')}
      </button>
    </div>
  );
}

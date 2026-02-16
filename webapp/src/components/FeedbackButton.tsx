import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';

export function FeedbackButton() {
  const navigate = useNavigate();
  const { t } = useTranslation();

  return (
    <button
      onClick={() => navigate('/issues')}
      className="text-sm text-text-secondary hover:text-text-primary transition-colors"
    >
      {t('common:feedback', 'Feedback')}
    </button>
  );
}

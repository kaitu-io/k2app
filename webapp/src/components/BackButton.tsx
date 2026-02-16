import { useNavigate } from 'react-router-dom';
import { ChevronLeft } from 'lucide-react';
import { useTranslation } from 'react-i18next';

export function BackButton() {
  const navigate = useNavigate();
  const { t } = useTranslation();

  return (
    <button
      onClick={() => navigate(-1)}
      className="flex items-center gap-1 text-sm text-text-secondary py-2 px-1"
    >
      <ChevronLeft className="w-4 h-4" />
      <span>{t('common:back', 'Back')}</span>
    </button>
  );
}

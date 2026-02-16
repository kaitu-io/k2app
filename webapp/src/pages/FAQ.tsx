import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';

const faqCards = [
  { titleKey: 'faqConnectionTitle', descKey: 'faqConnectionDesc', icon: '🔌' },
  { titleKey: 'faqAccountTitle', descKey: 'faqAccountDesc', icon: '👤' },
  { titleKey: 'faqSpeedTitle', descKey: 'faqSpeedDesc', icon: '⚡' },
  { titleKey: 'faqSecurityTitle', descKey: 'faqSecurityDesc', icon: '🔒' },
] as const;

export function FAQ() {
  const { t } = useTranslation('feedback');
  const navigate = useNavigate();

  return (
    <div className="p-4 space-y-6">
      <h1 className="text-xl font-semibold">{t('title')}</h1>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        {faqCards.map((card) => (
          <div
            key={card.titleKey}
            className="rounded-lg p-4 bg-[--color-bg-paper] transition-transform hover:translate-y-[-2px]"
          >
            <div className="w-10 h-10 flex items-center justify-center text-[--color-primary] text-2xl mb-3">
              {card.icon}
            </div>
            <h3 className="font-medium text-[--color-text-primary] mb-1">
              {t(card.titleKey)}
            </h3>
            <p className="text-sm text-[--color-text-secondary]">
              {t(card.descKey)}
            </p>
          </div>
        ))}
      </div>

      <div className="flex gap-3 pt-2">
        <button
          onClick={() => navigate('/issues')}
          className="flex-1 py-2 px-4 rounded-lg border border-[--color-primary] text-[--color-primary] hover:bg-[--color-primary]/10 transition-colors"
        >
          {t('viewIssues')}
        </button>
        <button
          onClick={() => navigate('/submit-ticket')}
          className="flex-1 py-2 px-4 rounded-lg bg-[--color-primary] text-white hover:opacity-90 transition-opacity"
        >
          {t('submitTicket')}
        </button>
      </div>
    </div>
  );
}

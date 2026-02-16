import { useTranslation } from 'react-i18next';

export function InviteRule() {
  const { t } = useTranslation('invite');

  return (
    <div data-testid="invite-rules" className="bg-[--color-card-bg] rounded-lg p-4 space-y-3">
      <h3 className="text-sm font-medium">{t('rules.title')}</h3>
      <ul className="space-y-2 text-xs text-[--color-text-secondary]">
        <li className="flex items-start gap-2">
          <span className="shrink-0 mt-0.5 w-1.5 h-1.5 rounded-full bg-[--color-primary]" />
          {t('rules.rule1')}
        </li>
        <li className="flex items-start gap-2">
          <span className="shrink-0 mt-0.5 w-1.5 h-1.5 rounded-full bg-[--color-primary]" />
          {t('rules.rule2')}
        </li>
        <li className="flex items-start gap-2">
          <span className="shrink-0 mt-0.5 w-1.5 h-1.5 rounded-full bg-[--color-primary]" />
          {t('rules.rule3')}
        </li>
      </ul>
    </div>
  );
}

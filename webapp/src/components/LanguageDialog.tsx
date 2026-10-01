import { useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { languages, availableLanguages, filterLanguages, type LanguageCode } from '../i18n/i18n';
import SearchPickerDialog, { type PickerOption } from './SearchPickerDialog';

interface LanguageDialogProps {
  open: boolean;
  value: LanguageCode;
  onClose: () => void;
  onSelect: (lang: LanguageCode) => void;
}

/**
 * Searchable language picker. Lists only the languages the active brand
 * offers; the current language is pinned to the top so it stays visible
 * however long the list grows. Layout (keyboard-safe) lives in
 * SearchPickerDialog.
 */
export default function LanguageDialog({ open, value, onClose, onSelect }: LanguageDialogProps) {
  const { t } = useTranslation();

  const ordered = useMemo(
    () => [...availableLanguages].sort((a, b) => Number(b === value) - Number(a === value)),
    [value],
  );

  const getOptions = useCallback(
    (query: string): PickerOption[] =>
      filterLanguages(query, ordered).map((code) => {
        const { nativeName, englishName, dir } = languages[code];
        return {
          id: code,
          primary: (
            <span lang={code} dir={dir}>
              {nativeName}
            </span>
          ),
          secondary: englishName === nativeName ? undefined : englishName,
        };
      }),
    [ordered],
  );

  return (
    <SearchPickerDialog
      open={open}
      title={t('account:account.selectLanguage')}
      searchLabel={t('account:account.searchLanguage')}
      emptyText={t('account:account.noLanguageFound')}
      value={value}
      getOptions={getOptions}
      onClose={onClose}
      onSelect={(id) => onSelect(id as LanguageCode)}
    />
  );
}

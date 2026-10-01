import { useMemo, useState } from 'react';
import {
  Dialog,
  DialogTitle,
  DialogContent,
  TextField,
  InputAdornment,
  List,
  ListItemButton,
  ListItemText,
  Typography,
  Box,
} from '@mui/material';
import { Search as SearchIcon, Check as CheckIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { languages, availableLanguages, filterLanguages, type LanguageCode } from '../i18n/i18n';

interface LanguageDialogProps {
  open: boolean;
  value: LanguageCode;
  onClose: () => void;
  onSelect: (lang: LanguageCode) => void;
}

/**
 * Searchable language picker. Lists only the languages the active brand
 * offers; the current language is pinned to the top so it stays visible
 * however long the list grows.
 */
export default function LanguageDialog({ open, value, onClose, onSelect }: LanguageDialogProps) {
  const { t } = useTranslation();
  const [query, setQuery] = useState('');

  const ordered = useMemo(
    () => [...availableLanguages].sort((a, b) => Number(b === value) - Number(a === value)),
    [value],
  );
  const matches = useMemo(() => filterLanguages(query, ordered), [query, ordered]);

  const handleClose = () => {
    setQuery('');
    onClose();
  };

  return (
    <Dialog open={open} onClose={handleClose} fullWidth maxWidth="xs" scroll="paper">
      <DialogTitle sx={{ pb: 1 }}>{t('account:account.selectLanguage')}</DialogTitle>
      <Box sx={{ px: 3, pb: 1 }}>
        <TextField
          autoFocus
          fullWidth
          size="small"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t('account:account.searchLanguage')}
          inputProps={{ 'aria-label': t('account:account.searchLanguage') }}
          InputProps={{
            startAdornment: (
              <InputAdornment position="start">
                <SearchIcon fontSize="small" />
              </InputAdornment>
            ),
          }}
        />
      </Box>
      <DialogContent sx={{ px: 1, pt: 0, minHeight: 240 }}>
        {matches.length === 0 ? (
          <Typography variant="body2" color="text.secondary" sx={{ px: 2, py: 3, textAlign: 'center' }}>
            {t('account:account.noLanguageFound')}
          </Typography>
        ) : (
          <List disablePadding>
            {matches.map((code) => {
              const { nativeName, englishName, dir } = languages[code];
              const selected = code === value;
              return (
                <ListItemButton
                  key={code}
                  selected={selected}
                  onClick={() => {
                    setQuery('');
                    onSelect(code);
                  }}
                  sx={{ borderRadius: 1.5 }}
                >
                  <ListItemText
                    primary={
                      <span lang={code} dir={dir}>
                        {nativeName}
                      </span>
                    }
                    secondary={englishName === nativeName ? undefined : englishName}
                    primaryTypographyProps={{ fontSize: '0.9rem', fontWeight: selected ? 600 : 400 }}
                    secondaryTypographyProps={{ fontSize: '0.75rem' }}
                  />
                  {selected && <CheckIcon fontSize="small" color="primary" />}
                </ListItemButton>
              );
            })}
          </List>
        )}
      </DialogContent>
    </Dialog>
  );
}

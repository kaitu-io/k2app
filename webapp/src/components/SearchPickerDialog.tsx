import { useState, type ReactNode } from 'react';
import {
  Box,
  Dialog,
  DialogContent,
  DialogTitle,
  InputAdornment,
  List,
  ListItemButton,
  ListItemText,
  TextField,
  Typography,
} from '@mui/material';
import { Search as SearchIcon, Check as CheckIcon } from '@mui/icons-material';
import { useVisibleViewportHeight } from '../hooks/useVisibleViewportHeight';

export interface PickerOption {
  id: string;
  primary: ReactNode;
  secondary?: string;
}

interface SearchPickerDialogProps {
  open: boolean;
  title: string;
  searchLabel: string;
  emptyText: string;
  /** Currently selected option id (gets the check mark). */
  value: string | null;
  /** Options for a query, already ordered. Called on every keystroke. */
  getOptions: (query: string) => PickerOption[];
  onClose: () => void;
  onSelect: (id: string) => void;
  /** Prefix for each row's data-testid (`${prefix}${id}`). */
  optionTestIdPrefix?: string;
}

/** Gap kept between the dialog and the edges of the visible area. */
const EDGE = 12;

/**
 * Search-then-pick dialog for long lists (languages, countries).
 *
 * Built around the on-screen keyboard, because the search box is focused the
 * moment it opens: the dialog is pinned to the TOP of the screen and its
 * height is capped to what is visible above the keyboard, so the result list
 * scrolls inside the visible area. A centred dialog (or a dropdown hanging
 * below its input) puts the lower rows under the keyboard where they can be
 * neither seen nor tapped.
 */
export default function SearchPickerDialog({
  open,
  title,
  searchLabel,
  emptyText,
  value,
  getOptions,
  onClose,
  onSelect,
  optionTestIdPrefix,
}: SearchPickerDialogProps) {
  const [query, setQuery] = useState('');
  const visibleHeight = useVisibleViewportHeight();
  const options = getOptions(query);

  const handleClose = () => {
    setQuery('');
    onClose();
  };

  return (
    <Dialog
      open={open}
      onClose={handleClose}
      fullWidth
      maxWidth="xs"
      scroll="paper"
      sx={{ '& .MuiDialog-container': { alignItems: 'flex-start' } }}
      PaperProps={{
        sx: {
          m: `${EDGE}px`,
          mt: `calc(env(safe-area-inset-top, 0px) + ${EDGE}px)`,
          width: `calc(100% - ${EDGE * 2}px)`,
          // The safe-area inset is subtracted too, so the bottom edge clears
          // the keyboard by EDGE on notched phones as well.
          maxHeight: `calc(${visibleHeight}px - env(safe-area-inset-top, 0px) - ${EDGE * 2}px)`,
        },
      }}
    >
      <DialogTitle sx={{ pb: 1 }}>{title}</DialogTitle>
      <Box sx={{ px: 3, pb: 1 }}>
        <TextField
          autoFocus
          fullWidth
          size="small"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={searchLabel}
          inputProps={{ 'aria-label': searchLabel, autoCapitalize: 'off', autoCorrect: 'off' }}
          InputProps={{
            startAdornment: (
              <InputAdornment position="start">
                <SearchIcon fontSize="small" />
              </InputAdornment>
            ),
          }}
        />
      </Box>
      <DialogContent sx={{ px: 1, pt: 0, pb: 1, overscrollBehavior: 'contain' }}>
        {options.length === 0 ? (
          <Typography variant="body2" color="text.secondary" sx={{ px: 2, py: 3, textAlign: 'center' }}>
            {emptyText}
          </Typography>
        ) : (
          <List disablePadding>
            {options.map((opt) => {
              const selected = opt.id === value;
              return (
                <ListItemButton
                  key={opt.id}
                  selected={selected}
                  data-testid={optionTestIdPrefix ? `${optionTestIdPrefix}${opt.id}` : undefined}
                  onClick={() => {
                    setQuery('');
                    onSelect(opt.id);
                  }}
                  sx={{ borderRadius: 1.5 }}
                >
                  <ListItemText
                    primary={opt.primary}
                    secondary={opt.secondary}
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

import { useState, useEffect } from 'react';
import {
  Popover,
  Box,
  Typography,
  RadioGroup,
  FormControlLabel,
  Radio,
  Alert,
} from '@mui/material';
import { useTranslation } from 'react-i18next';

interface ExpirationSelectorPopoverProps {
  anchorEl: HTMLElement | null;
  open: boolean;
  onClose: () => void;
  onSelect: (days: number) => void;
  defaultDays?: number;
}

export default function ExpirationSelectorPopover({
  anchorEl,
  open,
  onClose,
  onSelect,
  defaultDays = 7,
}: ExpirationSelectorPopoverProps) {
  const { t } = useTranslation();
  const [selectedDays, setSelectedDays] = useState(defaultDays);

  useEffect(() => {
    if (open) setSelectedDays(defaultDays);
  }, [open, defaultDays]);

  const handleSelect = (days: number) => {
    setSelectedDays(days);
    onSelect(days);
    onClose();
  };

  const options = [
    { label: t('invite:invite.expiration.1day'), days: 1 },
    { label: t('invite:invite.expiration.7days'), days: 7 },
    { label: t('invite:invite.expiration.30days'), days: 30 },
    { label: t('invite:invite.expiration.365days'), days: 365 },
  ];

  return (
    <Popover
      open={open}
      anchorEl={anchorEl}
      onClose={onClose}
      anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      transformOrigin={{ vertical: 'top', horizontal: 'center' }}
    >
      <Box sx={{ p: 2, maxWidth: 320 }}>
        <Typography variant="subtitle2" sx={{ mb: 1.5, fontWeight: 600 }}>
          {t('invite:invite.selectExpiration')}
        </Typography>

        <Alert severity="info" variant="outlined" sx={{ mb: 2, fontSize: '0.8rem' }}>
          {t('invite:invite.expirationSecurityHint')}
        </Alert>

        <RadioGroup value={selectedDays}>
          {options.map((option) => (
            <FormControlLabel
              key={option.days}
              value={option.days}
              control={<Radio />}
              label={option.label}
              onChange={() => handleSelect(option.days)}
            />
          ))}
        </RadioGroup>
      </Box>
    </Popover>
  );
}

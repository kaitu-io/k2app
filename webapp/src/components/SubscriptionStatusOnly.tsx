import { Box, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';

/**
 * Membership block for platforms where no purchase surface may exist
 * (purchaseSurfaceAvailable() === false and not iOS — i.e. a Google-Play-only
 * brand's Android build). Status + expiry only: no buttons, no links, no
 * prices, no "go to the website" hint (Play Payments policy).
 */
export default function SubscriptionStatusOnly({ isPro, expiresText }: { isPro: boolean; expiresText: string }) {
  const { t } = useTranslation();
  return (
    <Box sx={{ py: 1 }}>
      <Typography variant="body2">
        {isPro ? t('account:account.currentPlan') : t('account:account.freePlan')}
      </Typography>
      {isPro && (
        <Typography variant="caption" color="text.secondary">
          {t('account:account.expiresOn', { date: expiresText })}
        </Typography>
      )}
      <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 0.5 }}>
        {t('account:account.managedElsewhere')}
      </Typography>
    </Box>
  );
}

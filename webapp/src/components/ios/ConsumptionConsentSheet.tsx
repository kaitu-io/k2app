/**
 * ConsumptionConsentSheet — 退款同意弹层（设计稿 ④）。
 *
 * 首次成功授予订阅 / 试用后询问一次：退款时是否允许向 Apple 提供使用情况。
 * 「允许」「不允许」都关闭弹层并记录到服务端（三态，重装后不再问）；请求失败也关闭，
 * 但不写状态，下次成功购买后会再问。
 */
import { useState } from 'react';
import { Box, Button, Drawer, Link, Stack, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useAppLinks } from '../../hooks/useAppLinks';
import { setAppleConsumptionConsent } from './consumption-consent';

interface ConsumptionConsentSheetProps {
  open: boolean;
  onClose: () => void;
}

export default function ConsumptionConsentSheet({ open, onClose }: ConsumptionConsentSheetProps) {
  const { t } = useTranslation();
  const { links } = useAppLinks();
  const [busy, setBusy] = useState(false);

  const answer = async (consented: boolean) => {
    if (busy) return;
    setBusy(true);
    await setAppleConsumptionConsent(consented);
    setBusy(false);
    onClose();
  };

  const points = [
    t('purchase:purchase.iap.consentPoint1'),
    t('purchase:purchase.iap.consentPoint2'),
    t('purchase:purchase.iap.consentPoint3'),
  ];

  // 文案里 <privacy>…</privacy> 标出隐私政策链接的位置（各语言语序不同，故留在文案里）。
  const note = t('purchase:purchase.iap.consentNote');
  const m = /^([\s\S]*?)<privacy>([\s\S]*?)<\/privacy>([\s\S]*)$/.exec(note);
  const noteParts = m ? { before: m[1], link: m[2], after: m[3] } : { before: note, link: '', after: '' };

  return (
    <Drawer
      anchor="bottom"
      open={open}
      onClose={onClose}  // 点遮罩关闭 = 没回答，不写状态，下次成功购买再问
      PaperProps={{ sx: { borderRadius: '20px 20px 0 0', px: 2.5, pt: 3, pb: 4 } }}
      data-testid="consumption-consent-sheet"
    >
      <Stack spacing={1.75} role="dialog" aria-labelledby="consent-title">
        <Box sx={{ alignSelf: 'center', width: 36, height: 5, borderRadius: 1, bgcolor: 'divider' }} />
        <Typography id="consent-title" component="h2" sx={{ m: 0, fontSize: 20, fontWeight: 700 }}>
          {t('purchase:purchase.iap.consentTitle')}
        </Typography>
        <Typography color="text.secondary" sx={{ fontSize: 15, lineHeight: 1.55 }}>
          {t('purchase:purchase.iap.consentBody')}
        </Typography>
        <Stack spacing={1}>
          {points.map((text) => (
            <Box key={text} sx={{ display: 'flex', gap: 1.25, fontSize: 14, color: 'text.secondary' }}>
              <Box component="span" sx={{ color: 'primary.main' }}>·</Box>
              {text}
            </Box>
          ))}
        </Stack>
        <Typography color="text.secondary" sx={{ fontSize: 13, lineHeight: 1.5 }}>
          {noteParts.before}
          <Link
            component="button"
            type="button"
            underline="always"
            sx={{ fontSize: 'inherit', verticalAlign: 'baseline' }}
            onClick={() => void window._platform?.openExternal?.(links.privacyPolicyUrl)}
          >
            {noteParts.link}
          </Link>
          {noteParts.after}
        </Typography>
        <Stack spacing={1.25} sx={{ mt: 0.5 }}>
          <Button
            variant="contained"
            color="primary"
            size="large"
            fullWidth
            disableElevation
            disabled={busy}
            onClick={() => void answer(true)}
            sx={{
              fontWeight: 700,
              fontSize: 16,
              minHeight: 50,
              borderRadius: 3,
              textTransform: 'none',
              bgcolor: 'primary.main',
              color: 'primary.contrastText',
              '&:hover': { bgcolor: 'primary.dark' },
            }}
          >
            {t('purchase:purchase.iap.consentAllow')}
          </Button>
          <Button
            fullWidth
            disabled={busy}
            onClick={() => void answer(false)}
            sx={{ fontWeight: 600, fontSize: 16, minHeight: 50, borderRadius: 3, textTransform: 'none', color: 'text.primary' }}
          >
            {t('purchase:purchase.iap.consentDeny')}
          </Button>
        </Stack>
      </Stack>
    </Drawer>
  );
}

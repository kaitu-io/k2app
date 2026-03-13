import { useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Box, Typography } from '@mui/material';
import BackButton from '../../components/BackButton';
import AndroidInstallStepper from './AndroidInstallStepper';

export default function AndroidInstall() {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();

  // URL params for third-party apps (from Discover iframe bridge_navigate)
  const name = searchParams.get('name') || 'Kaitu';
  const icon = searchParams.get('icon') || '/favicon.png';
  const desc = searchParams.get('desc') || '';
  const apkUrl = searchParams.get('apk') || ''; // "" = Kaitu default (daemon fetches latest.json)

  // Desktop-only check
  const isDesktop = window._platform?.os === 'macos' || window._platform?.os === 'windows' || window._platform?.os === 'linux';

  if (!isDesktop) {
    return (
      <Box sx={{ p: 3 }}>
        <BackButton to="/account" />
        <Typography variant="h6" sx={{ mt: 2 }}>
          {t('purchase:androidInstall.desktopOnly')}
        </Typography>
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3, maxWidth: 720, mx: 'auto' }}>
      <BackButton to="/device-install" />

      <Typography variant="h5" sx={{ mt: 2, mb: 0.5, fontWeight: 600 }}>
        {name === 'Kaitu' ? t('purchase:androidInstall.title') : name}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
        {t('purchase:androidInstall.subtitle')}
      </Typography>

      <AndroidInstallStepper
        name={name}
        icon={icon}
        desc={desc}
        apkUrl={apkUrl}
      />
    </Box>
  );
}

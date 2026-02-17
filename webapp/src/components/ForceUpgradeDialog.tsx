import { useState, useEffect, useMemo } from 'react';
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  Box,
  Typography,
  Avatar
} from '@mui/material';
import {
  SystemUpdateAlt as UpgradeIcon
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useAppConfig } from '../hooks/useAppConfig';
import { isOlderVersion, isValidVersion, cleanVersion } from '../utils/versionCompare';

/**
 * ForceUpgradeDialog - Shows a modal dialog when the client version is below minClientVersion.
 * This dialog cannot be dismissed - the user must download the new version.
 * Checks version from appConfig.minClientVersion against platform.getVersion().
 */
const ForceUpgradeDialog: React.FC = () => {
  const { t } = useTranslation();
  const { appConfig } = useAppConfig();

  const [open, setOpen] = useState(false);
  const [appVersion, setAppVersion] = useState<string>('');
  const [minVersion, setMinVersion] = useState<string>('');

  // Build download URL from appLinks
  const downloadUrl = useMemo(() => {
    if (!appConfig?.appLinks) {
      return 'https://kaitu.io/install'; // fallback
    }
    const { baseURL, installPath } = appConfig.appLinks;
    return `${baseURL || 'https://kaitu.io'}${installPath || '/install'}`;
  }, [appConfig?.appLinks]);

  // Check version when app config is loaded
  useEffect(() => {
    if (!appConfig?.minClientVersion) return;

    const checkVersion = async () => {
      try {
        const currentVersion = window._platform!.version;
        const minClientVersion = appConfig.minClientVersion;

        if (!minClientVersion) return;

        // Clean version strings
        const cleanCurrentVersion = cleanVersion(currentVersion);
        const cleanMinVersion = cleanVersion(minClientVersion);

        // Skip if either version is invalid
        if (!isValidVersion(cleanCurrentVersion) || !isValidVersion(cleanMinVersion)) {
          console.warn('[ForceUpgradeDialog] Invalid version format: ' + JSON.stringify({
            current: cleanCurrentVersion,
            min: cleanMinVersion
          }));
          return;
        }

        // Check if current version is older than minimum required
        if (isOlderVersion(cleanCurrentVersion, cleanMinVersion)) {
          console.info('[ForceUpgradeDialog] Upgrade required: ' + JSON.stringify({
            current: cleanCurrentVersion,
            min: cleanMinVersion
          }));
          setAppVersion(cleanCurrentVersion);
          setMinVersion(cleanMinVersion);
          setOpen(true);
        }
      } catch (error) {
        console.error('[ForceUpgradeDialog] Version check failed:', error);
      }
    };

    checkVersion();
  }, [appConfig?.minClientVersion]);

  const handleDownload = async () => {
    try {
      // Use platform's openExternal method to open link in external browser
      if (window._platform!.openExternal) {
        await window._platform!.openExternal(downloadUrl);
      } else {
        // Fallback to window.open (Web environment)
        window.open(downloadUrl, '_blank', 'noopener,noreferrer');
      }
    } catch (error) {
      console.error('Failed to open download URL:', error);
      // Also try window.open on error
      window.open(downloadUrl, '_blank', 'noopener,noreferrer');
    }
    // Note: Don't close the dialog - user must update
  };

  return (
    <Dialog
      open={open}
      maxWidth="sm"
      fullWidth
      disableEscapeKeyDown
      onClose={() => {}} // Prevent closing by clicking outside
      sx={{
        '& .MuiDialog-paper': {
          borderRadius: 2,
          m: 2,
          maxHeight: 'calc(100vh - 32px)'
        }
      }}
    >
      {/* Header */}
      <DialogTitle sx={{ pb: 2, pt: 3, px: 3 }}>
        <Box display="flex" flexDirection="column" alignItems="center" gap={2}>
          <Avatar
            sx={{
              bgcolor: 'error.main',
              width: 56,
              height: 56
            }}
          >
            <UpgradeIcon sx={{ fontSize: 32 }} />
          </Avatar>
          <Typography variant="h6" fontWeight="600" color="text.primary" textAlign="center">
            {t('startup:forceUpgrade.title')}
          </Typography>
        </Box>
      </DialogTitle>

      <DialogContent sx={{ px: 3, pb: 2 }}>
        <Typography variant="body2" color="text.secondary" sx={{ textAlign: 'center', lineHeight: 1.6 }}>
          {t('startup:forceUpgrade.description', {
            currentVersion: appVersion,
            minVersion: minVersion
          })}
        </Typography>
      </DialogContent>

      <DialogActions sx={{ px: 2.5, pb: 2.5, pt: 1.5 }}>
        <Button
          onClick={handleDownload}
          variant="contained"
          color="error"
          size="medium"
          autoFocus
          fullWidth
          startIcon={<UpgradeIcon />}
          sx={{
            borderRadius: 1.5,
            py: 1
          }}
        >
          {t('startup:forceUpgrade.downloadButton')}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

export default ForceUpgradeDialog;

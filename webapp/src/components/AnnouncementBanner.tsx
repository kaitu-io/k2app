import { useState, useEffect, useMemo } from 'react';
import {
  Box,
  Typography,
  IconButton,
  Link,
  keyframes
} from '@mui/material';
import {
  Close as CloseIcon,
  Campaign as CampaignIcon
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useAppConfig } from '../hooks/useAppConfig';
import type { Announcement } from '../services/api-types';

// Storage key prefix for dismissed announcements
const STORAGE_KEY_PREFIX = 'announcement_dismissed_';

// Marquee animation keyframes
const marquee = keyframes`
  0% {
    transform: translateX(100%);
  }
  100% {
    transform: translateX(-100%);
  }
`;

/**
 * Check if an announcement has been dismissed
 */
function isAnnouncementDismissed(id: string): boolean {
  try {
    const dismissed = localStorage.getItem(`${STORAGE_KEY_PREFIX}${id}`);
    return dismissed === 'true';
  } catch {
    return false;
  }
}

/**
 * Mark an announcement as dismissed
 */
function dismissAnnouncement(id: string): void {
  try {
    localStorage.setItem(`${STORAGE_KEY_PREFIX}${id}`, 'true');
  } catch {
    // Ignore storage errors
  }
}

/**
 * Check if an announcement has expired
 */
function isAnnouncementExpired(expiresAt?: number): boolean {
  if (!expiresAt || expiresAt === 0) return false;
  return Date.now() / 1000 > expiresAt;
}

/**
 * AnnouncementBanner - Shows a scrolling marquee banner for announcements.
 * Features:
 * - Marquee-style scrolling text
 * - Optional clickable link
 * - Dismissible (persisted in localStorage by announcement ID)
 * - Respects expiration time
 */
const AnnouncementBanner: React.FC = () => {
  const { t } = useTranslation();
  const { appConfig } = useAppConfig();

  const [visible, setVisible] = useState(false);
  const [announcement, setAnnouncement] = useState<Announcement | null>(null);

  // Check and display announcement
  useEffect(() => {
    if (!appConfig?.announcement) {
      setVisible(false);
      return;
    }

    const ann = appConfig.announcement;

    // Check if expired
    if (isAnnouncementExpired(ann.expiresAt)) {
      console.info('[AnnouncementBanner] Announcement expired:', ann.id);
      setVisible(false);
      return;
    }

    // Check if dismissed
    if (isAnnouncementDismissed(ann.id)) {
      console.info('[AnnouncementBanner] Announcement already dismissed:', ann.id);
      setVisible(false);
      return;
    }

    // Show announcement
    setAnnouncement(ann);
    setVisible(true);
  }, [appConfig?.announcement]);

  const handleDismiss = () => {
    if (announcement) {
      dismissAnnouncement(announcement.id);
    }
    setVisible(false);
  };

  const handleLinkClick = async (e: React.MouseEvent) => {
    e.preventDefault();
    if (!announcement?.linkUrl) return;

    try {
      await window._platform!.openExternal(announcement.linkUrl);
    } catch (error) {
      console.error('Failed to open link:', error);
      window.open(announcement.linkUrl, '_blank', 'noopener,noreferrer');
    }
  };

  // Calculate animation duration based on message length
  const animationDuration = useMemo(() => {
    if (!announcement?.message) return 10;
    // Approximate: 30 characters per second
    const duration = Math.max(8, Math.ceil(announcement.message.length / 15));
    return duration;
  }, [announcement?.message]);

  if (!visible || !announcement) {
    return null;
  }

  return (
    <Box
      sx={{
        position: 'relative',
        width: '100%',
        bgcolor: 'primary.main',
        color: 'primary.contrastText',
        py: 0.75,
        px: 1,
        display: 'flex',
        alignItems: 'center',
        overflow: 'hidden',
        zIndex: 1100,
      }}
    >
      {/* Campaign icon */}
      <Box
        sx={{
          flexShrink: 0,
          display: 'flex',
          alignItems: 'center',
          mr: 1,
          zIndex: 1,
          bgcolor: 'primary.main',
          pr: 1,
        }}
      >
        <CampaignIcon sx={{ fontSize: 18 }} />
      </Box>

      {/* Marquee container */}
      <Box
        sx={{
          flex: 1,
          overflow: 'hidden',
          position: 'relative',
        }}
      >
        <Box
          sx={{
            display: 'inline-block',
            whiteSpace: 'nowrap',
            animation: `${marquee} ${animationDuration}s linear infinite`,
            '&:hover': {
              animationPlayState: 'paused',
            },
          }}
        >
          <Typography
            variant="body2"
            component="span"
            sx={{
              fontSize: '0.85rem',
              fontWeight: 500,
            }}
          >
            {announcement.message}
          </Typography>

          {/* Optional link */}
          {announcement.linkUrl && (
            <Link
              href={announcement.linkUrl}
              onClick={handleLinkClick}
              sx={{
                ml: 1.5,
                color: 'inherit',
                textDecoration: 'underline',
                cursor: 'pointer',
                '&:hover': {
                  opacity: 0.8,
                },
              }}
            >
              {announcement.linkText || t('common:common.viewDetails')}
            </Link>
          )}
        </Box>
      </Box>

      {/* Close button */}
      <IconButton
        size="small"
        onClick={handleDismiss}
        sx={{
          flexShrink: 0,
          color: 'inherit',
          ml: 1,
          p: 0.25,
          zIndex: 1,
          bgcolor: 'primary.main',
          '&:hover': {
            bgcolor: 'primary.dark',
          },
        }}
        aria-label={t('common:common.close')}
      >
        <CloseIcon sx={{ fontSize: 18 }} />
      </IconButton>
    </Box>
  );
};

export default AnnouncementBanner;

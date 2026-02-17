import { useState, useEffect, useRef } from 'react';
import {
  ListItem,
  ListItemIcon,
  ListItemText,
  Typography,
  Box,
} from '@mui/material';
import { BuildCircle as BuildCircleIcon, ChevronRight as ChevronRightIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';

// localStorage key for developer mode
const DEV_MODE_STORAGE_KEY = 'kaitu_dev_mode';

interface VersionItemProps {
  appVersion: string;
  onDevModeActivated?: () => void;
}

export default function VersionItem({ appVersion, onDevModeActivated }: VersionItemProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [clickCount, setClickCount] = useState(0);
  const timeoutRef = useRef<NodeJS.Timeout | null>(null);

  // Reset click counter (3 seconds without action)
  useEffect(() => {
    if (clickCount > 0 && clickCount < 7) {
      if (timeoutRef.current) {
        clearTimeout(timeoutRef.current);
      }

      timeoutRef.current = setTimeout(() => {
        setClickCount(0);
      }, 3000);
    }

    return () => {
      if (timeoutRef.current) {
        clearTimeout(timeoutRef.current);
      }
    };
  }, [clickCount]);

  // Handle click on entire row -> navigate to changelog
  const handleRowClick = () => {
    navigate('/changelog');
  };

  // Handle click on version number -> count for dev mode activation
  const handleVersionClick = async (e: React.MouseEvent) => {
    e.stopPropagation(); // Prevent row click

    const newCount = clickCount + 1;
    setClickCount(newCount);

    // 7 consecutive clicks activates developer mode (Android-style easter egg)
    if (newCount === 7) {
      try {
        // Enable TRACE logging via set_config
        await window._k2.run('set_config', { log: { level: 'TRACE' } });
        console.info('[VersionItem] Developer mode activated, TRACE logging enabled');

        // Store dev mode in localStorage
        localStorage.setItem(DEV_MODE_STORAGE_KEY, 'true');

        // Notify parent and navigate to developer settings
        onDevModeActivated?.();
        navigate('/developer-settings');
      } catch (error) {
        console.error('[VersionItem] Failed to activate developer mode:', error);
      } finally {
        setClickCount(0);
      }
    }
  };

  return (
    <ListItem
      sx={{
        py: 1.5,
        cursor: 'pointer',
        '&:hover': {
          backgroundColor: 'action.hover',
        },
      }}
      onClick={handleRowClick}
      secondaryAction={<ChevronRightIcon color="action" />}
    >
      <ListItemIcon>
        <BuildCircleIcon />
      </ListItemIcon>
      <ListItemText
        primary={
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <Typography variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
              {t('account:account.appVersion')}
            </Typography>
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{
                fontWeight: 500,
                fontSize: '0.75rem',
                cursor: 'pointer',
                userSelect: 'none',
                px: 0.5,
                borderRadius: 0.5,
                '&:active': {
                  backgroundColor: 'action.selected',
                },
              }}
              onClick={handleVersionClick}
            >
              {appVersion}
            </Typography>
          </Box>
        }
      />
    </ListItem>
  );
}

// Helper to check if dev mode is enabled
export function isDevModeEnabled(): boolean {
  return localStorage.getItem(DEV_MODE_STORAGE_KEY) === 'true';
}

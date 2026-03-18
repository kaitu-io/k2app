import { useCallback, useRef, useState } from 'react';
import {
  ListItem,
  ListItemIcon,
  ListItemText,
  Typography,
  Box,
  Select,
  MenuItem,
  FormControl,
  Divider,
  Chip,
} from '@mui/material';
import {
  BuildCircle as BuildCircleIcon,
  ChevronRight as ChevronRightIcon,
  BugReport as BugReportIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useAlertStore } from '../stores';

const DEV_MODE_KEY = 'k2_developer_mode';
const LOG_LEVEL_KEY = 'k2_log_level';
const TAP_COUNT = 7;
const TAP_WINDOW_MS = 3000;

type LogLevel = 'debug' | 'info' | 'warn' | 'error';

interface VersionItemProps {
  appVersion: string;
  commit?: string;
}

export default function VersionItem({ appVersion, commit }: VersionItemProps) {
  const { t } = useTranslation('account');
  const navigate = useNavigate();
  const tapCountRef = useRef(0);
  const tapTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [isDeveloperMode, setIsDeveloperMode] = useState(
    () => localStorage.getItem(DEV_MODE_KEY) === 'true'
  );
  const [logLevel, setLogLevel] = useState<LogLevel>(
    () => (localStorage.getItem(LOG_LEVEL_KEY) as LogLevel) || 'info'
  );

  const handleVersionTap = useCallback((e: React.MouseEvent) => {
    e.stopPropagation();

    if (isDeveloperMode) return; // Already activated

    tapCountRef.current++;

    if (tapTimerRef.current) clearTimeout(tapTimerRef.current);
    tapTimerRef.current = setTimeout(() => {
      tapCountRef.current = 0;
    }, TAP_WINDOW_MS);

    if (tapCountRef.current >= TAP_COUNT) {
      tapCountRef.current = 0;
      localStorage.setItem(DEV_MODE_KEY, 'true');
      setIsDeveloperMode(true);
      window._platform?.setDevEnabled?.(true);
      window._platform?.setLogLevel?.('debug');
      localStorage.setItem(LOG_LEVEL_KEY, 'debug');
      setLogLevel('debug');
      useAlertStore.getState().showAlert(t('developerModeActivated'), 'success');
    }
  }, [isDeveloperMode, t]);

  const handleLogLevelChange = useCallback((e: any) => {
    const level = e.target.value as LogLevel;
    setLogLevel(level);
    window._platform?.setLogLevel?.(level);
  }, []);

  return (
    <>
      <ListItem
        sx={{
          py: 1.5,
          cursor: 'pointer',
          '&:hover': { backgroundColor: 'action.hover' },
        }}
        onClick={() => navigate('/changelog')}
        secondaryAction={<ChevronRightIcon color="action" />}
      >
        <ListItemIcon>
          <BuildCircleIcon />
        </ListItemIcon>
        <ListItemText
          primary={
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
              <Typography variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
                {t('account.appVersion')}
              </Typography>
              <Typography
                variant="caption"
                color="text.secondary"
                onClick={handleVersionTap}
                sx={{ fontWeight: 500, fontSize: '0.75rem', userSelect: 'none' }}
              >
                {appVersion}{commit ? ` (${commit})` : ''}
              </Typography>
              {appVersion.includes('-beta') && (
                <Chip
                  label={t('betaProgram.badge')}
                  color="warning"
                  size="small"
                  sx={{ height: 18, fontSize: '0.65rem', fontWeight: 600 }}
                />
              )}
            </Box>
          }
        />
      </ListItem>

      {isDeveloperMode && (
        <>
          <Divider />
          <ListItem sx={{ py: 1.5 }}>
            <ListItemIcon>
              <BugReportIcon />
            </ListItemIcon>
            <ListItemText
              primary={
                <Typography variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
                  {t('logLevel')}
                </Typography>
              }
            />
            <FormControl size="small" sx={{ minWidth: 110 }}>
              <Select
                value={logLevel}
                onChange={handleLogLevelChange}
                variant="outlined"
                sx={{ borderRadius: 1.5, '& .MuiSelect-select': { py: 1 } }}
              >
                <MenuItem value="debug">debug</MenuItem>
                <MenuItem value="info">info</MenuItem>
                <MenuItem value="warn">warn</MenuItem>
                <MenuItem value="error">error</MenuItem>
              </Select>
            </FormControl>
          </ListItem>
        </>
      )}
    </>
  );
}

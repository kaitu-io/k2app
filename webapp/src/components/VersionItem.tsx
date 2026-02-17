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

interface VersionItemProps {
  appVersion: string;
}

export default function VersionItem({ appVersion }: VersionItemProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();

  // Handle click on entire row -> navigate to changelog
  const handleRowClick = () => {
    navigate('/changelog');
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
              }}
            >
              {appVersion}
            </Typography>
          </Box>
        }
      />
    </ListItem>
  );
}

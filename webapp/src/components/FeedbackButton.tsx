/**
 * FeedbackButton - Floating feedback button with attention-grabbing animation
 *
 * Features:
 * - Fixed position at top-left corner
 * - Pulsing animation to attract user attention
 * - Navigates to feedback form page on click
 */

import { useNavigate } from 'react-router-dom';
import { Fab, Tooltip, useTheme, keyframes } from '@mui/material';
import { Feedback as FeedbackIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';

// Pulsing animation for attention (orange glow)
const pulse = keyframes`
  0% {
    box-shadow: 0 0 0 0 rgba(255, 152, 0, 0.7);
  }
  70% {
    box-shadow: 0 0 0 12px rgba(255, 152, 0, 0);
  }
  100% {
    box-shadow: 0 0 0 0 rgba(255, 152, 0, 0);
  }
`;

export default function FeedbackButton() {
  const { t } = useTranslation();
  const theme = useTheme();
  const navigate = useNavigate();

  const handleClick = () => {
    navigate('/submit-ticket?feedback=true');
  };

  return (
    <Tooltip title={t('feedback:feedback.buttonTooltip')} placement="right" arrow>
      <Fab
        size="small"
        onClick={handleClick}
        sx={{
          position: 'fixed',
          left: 8,
          top: 84,
          zIndex: theme.zIndex.fab,
          bgcolor: 'warning.main',
          color: 'white',
          animation: `${pulse} 2s infinite`,
          '&:hover': {
            bgcolor: 'warning.dark',
            transform: 'scale(1.1)',
            animation: 'none',
          },
          transition: 'transform 0.2s',
        }}
        aria-label={t('feedback:feedback.buttonLabel')}
      >
        <FeedbackIcon />
      </Fab>
    </Tooltip>
  );
}

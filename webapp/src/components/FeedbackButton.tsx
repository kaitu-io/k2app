import { useNavigate } from 'react-router-dom';
import { Fab, Tooltip, useTheme, keyframes } from '@mui/material';
import { Feedback as FeedbackIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useDraggable } from '../hooks/useDraggable';
import { useLayout } from '../stores';

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
  const { sidebarWidth, isDesktop } = useLayout();

  const { position, isDragging, bindDrag, elementRef } = useDraggable({
    storageKey: 'k2_feedback_btn_pos',
    defaultY: 84,
    defaultSide: 'left',
    edgeMargin: 8,
    elementSize: 40, // MUI Fab size="small"
    dragThreshold: 5,
    sidebarWidth: isDesktop ? sidebarWidth : 0,
  });

  const handleClick = () => {
    if (isDragging) return;
    navigate('/submit-ticket?feedback=true');
  };

  return (
    <Tooltip
      title={t('feedback:feedback.buttonTooltip')}
      placement={position.side === 'left' ? 'right' : 'left'}
      arrow
      open={isDragging ? false : undefined}
      disableInteractive
    >
      <Fab
        ref={elementRef as React.Ref<HTMLButtonElement>}
        size="small"
        onClick={handleClick}
        {...bindDrag}
        style={{
          left: position.x,
          top: position.y,
        }}
        sx={{
          position: 'fixed',
          zIndex: theme.zIndex.fab,
          bgcolor: 'warning.main',
          color: 'white',
          touchAction: 'none',
          userSelect: 'none',
          animation: isDragging ? 'none' : `${pulse} 2s infinite`,
          cursor: isDragging ? 'grabbing' : 'pointer',
          '&:hover': isDragging ? {} : {
            bgcolor: 'warning.dark',
            transform: 'scale(1.1)',
            animation: 'none',
          },
          transition: isDragging ? 'none' : 'transform 0.2s',
        }}
        aria-label={t('feedback:feedback.buttonLabel')}
      >
        <FeedbackIcon />
      </Fab>
    </Tooltip>
  );
}

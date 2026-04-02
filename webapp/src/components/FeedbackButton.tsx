import { useNavigate } from 'react-router-dom';
import { Badge, Fab, Portal, Tooltip, useTheme, keyframes } from '@mui/material';
import { Feedback as FeedbackIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useDraggable } from '../hooks/useDraggable';
import { useLayout } from '../stores';
import { useFeedbackStore } from '../stores/feedback.store';

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
  const unreadCount = useFeedbackStore((s) => s.unreadCount);

  const { position, isDragging, bindDrag, elementRef } = useDraggable({
    storageKey: 'k2_feedback_btn_pos',
    defaultY: Math.round(window.innerHeight * 0.65),
    defaultSide: 'right',
    edgeMargin: 8,
    elementSize: 40, // MUI Fab size="small"
    dragThreshold: 5,
    sidebarWidth: isDesktop ? sidebarWidth : 0,
  });

  const handleClick = () => {
    if (isDragging) return;
    navigate('/feedback');
  };

  return (
    <Portal>
      <Tooltip
        title={t('feedback:feedback.buttonTooltip')}
        placement={position.side === 'left' ? 'right' : 'left'}
        arrow
        open={isDragging ? false : undefined}
        disableInteractive
      >
        <Fab
          ref={elementRef}
          size="small"
          onClick={handleClick}
          data-tour="feedback-button"
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
          <Badge
            badgeContent={unreadCount}
            color="error"
            invisible={unreadCount === 0}
            sx={{ '& .MuiBadge-badge': { fontSize: '0.65rem', height: 16, minWidth: 16 } }}
          >
            <FeedbackIcon />
          </Badge>
        </Fab>
      </Tooltip>
    </Portal>
  );
}

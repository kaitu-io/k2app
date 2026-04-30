/**
 * SmartModeIcon — 智能模式图标徽章
 * 统一尺寸 32×22，统一样式，全局唯一真相来源。
 */

import { Box } from '@mui/material';
import PublicIcon from '@mui/icons-material/Public';

export function SmartModeIcon() {
  return (
    <Box sx={{
      width: 32,
      height: 22,
      borderRadius: 0.5,
      background: '#ffffff',
      border: '1px solid rgba(0,0,0,0.12)',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      flexShrink: 0,
    }}>
      <PublicIcon sx={{
        fontSize: 18,
        color: '#1565c0',
        animation: 'smartSparkle 2s ease-in-out infinite',
        '@keyframes smartSparkle': {
          '0%, 100%': { opacity: 1, transform: 'scale(1)' },
          '50%': { opacity: 0.6, transform: 'scale(0.85)' },
        },
      }} />
    </Box>
  );
}

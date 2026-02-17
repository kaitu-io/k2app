import { createTheme, ThemeOptions } from '@mui/material/styles';

// 共享的主题配置
const sharedThemeConfig: ThemeOptions = {
  typography: {
    fontFamily: [
      '-apple-system',
      'BlinkMacSystemFont',
      '"Segoe UI"',
      'Roboto',
      '"Helvetica Neue"',
      'Arial',
      'sans-serif',
    ].join(','),
  },
  components: {
    MuiCssBaseline: {
      styleOverrides: `
        *, *::before, *::after {
          -webkit-user-select: none !important;
          -moz-user-select: none !important;
          -ms-user-select: none !important;
          user-select: none !important;
          -webkit-touch-callout: none !important;
        }
      `,
    },
    MuiDrawer: {
      styleOverrides: {
        paper: {
          width: 220,
        },
      },
    },
  },
};

// 浅色主题配置 - 与 Mobile 端保持一致的专业配色
export const lightTheme = createTheme({
  ...sharedThemeConfig,
  palette: {
    mode: 'light',
    primary: {
      main: '#1565C0', // 深蓝色，与 Mobile 端保持一致，专业感更强
      light: '#42A5F5', // 浅蓝色用于高亮
      dark: '#0D47A1',  // 更深蓝色用于 hover 状态
    },
    secondary: {
      main: '#00838F', // 青绿色，与 Mobile 端一致，体现连网特性
      light: '#26C6DA', // 浅青绿色
      dark: '#006064',  // 深青绿色
    },
    success: {
      main: '#2E7D32', // 成功/连接状态色，与 Mobile 端一致
      light: '#66BB6A',
      dark: '#1B5E20',
    },
    error: {
      main: '#C62828', // 错误/断连状态色，与 Mobile 端一致
      light: '#EF5350',
      dark: '#8E0000',
    },
    warning: {
      main: '#F57C00', // 警告状态色，与 Mobile 端一致
      light: '#FFB74D',
      dark: '#E65100',
    },
    background: {
      default: '#FAFAFA', // 与 Mobile 端完全一致的背景色
      paper: '#FFFFFF',   // 与 Mobile 端完全一致的纸质背景
    },
  },
  components: {
    ...sharedThemeConfig.components,
    MuiAppBar: {
      styleOverrides: {
        root: {
          backgroundImage: 'none',
          backgroundColor: '#1565C0', // 使用主品牌色，突出關鍵操作區域的重要性
          borderBottom: 'none', // 移除邊框，讓色彩本身承擔區分作用
          color: '#FFFFFF', // 白色文字確保在深色背景下清晰
          boxShadow: '0 2px 8px rgba(21, 101, 192, 0.15)', // 使用品牌色的陰影增強立體感
        },
      },
    },
    MuiButton: {
      styleOverrides: {
        root: {
          textTransform: 'none', // 保持按钮文字不全大写
        },
      },
    },
  },
});

// 深色主题配置 - 与 Mobile 端保持一致的专业深色配色
export const darkTheme = createTheme({
  ...sharedThemeConfig,
  palette: {
    mode: 'dark',
    primary: {
      main: '#42A5F5',  // 与 Mobile 端完全一致的浅蓝色，深色背景下可见性好
      light: '#90CAF9', // 更浅的蓝色用于高亮
      dark: '#1976D2',  // 深蓝色用于 hover 状态
    },
    secondary: {
      main: '#26C6DA',  // 与 Mobile 端一致的浅青绿色，深色模式下效果好
      light: '#4DD0E1', // 更浅的青绿色
      dark: '#0097A7',  // 深青绿色
    },
    success: {
      main: '#66BB6A',  // 与 Mobile 端一致的浅绿色，深色背景下可见
      light: '#81C784',
      dark: '#388E3C',
    },
    error: {
      main: '#EF5350',  // 与 Mobile 端一致的浅红色
      light: '#E57373',
      dark: '#C62828',
    },
    warning: {
      main: '#FFB74D',  // 与 Mobile 端一致的浅橙色
      light: '#FFCC02',
      dark: '#F57C00',
    },
    background: {
      default: '#0F0F13', // 统一深色背景 - 与 Web 端协调的深色调
      paper: '#1A1A1D',   // 统一纸质背景 - 与 Web 端 card 色调协调  
    },
    text: {
      primary: '#FAFAFA',                    // 统一主文本色 - 与 Web 端 foreground 一致
      secondary: 'rgba(250, 250, 250, 0.7)', // 统一次文本色 - 与主文本色协调
    },
    divider: 'rgba(255, 255, 255, 0.12)', // 统一分割线颜色 - 与 Web 端 border 协调
  },
  components: {
    ...sharedThemeConfig.components,
    MuiPaper: {
      styleOverrides: {
        root: {
          backgroundImage: 'none', // 移除深色模式下的默认渐变
        },
      },
    },
    MuiAppBar: {
      styleOverrides: {
        root: {
          backgroundImage: 'none',
          backgroundColor: '#1A1A1D', // 与 paper 背景一致，保持 header 的专业感
          borderBottom: '1px solid rgba(255, 255, 255, 0.12)', // 统一边框颜色
        },
      },
    },
    MuiButton: {
      styleOverrides: {
        root: {
          textTransform: 'none', // 保持按钮文字不全大写
        },
      },
    },
  },
});

// 語義化顏色輔助函數，與 Web 端統一協調
export const getSemanticColors = (isDark: boolean = false) => ({
  // VPN 連接狀態顏色  
  connectionStatus: {
    connected: isDark ? '#66BB6A' : '#2E7D32',      // 綠色 - 已連接
    disconnected: isDark ? '#FAFAFA' : '#79747E',   // 統一灰色 - 未連接
    connecting: isDark ? '#FFB74D' : '#F57C00',     // 橙色 - 連接中
    error: isDark ? '#EF5350' : '#C62828',          // 紅色 - 連接錯誤
  },
  
  // 服務器狀態顏色
  serverStatus: {
    online: isDark ? '#66BB6A' : '#2E7D32',         // 綠色 - 在線
    offline: isDark ? '#EF5350' : '#C62828',        // 紅色 - 離線
    warning: isDark ? '#FFB74D' : '#F57C00',        // 橙色 - 警告
  },
  
  // 會員狀態顏色
  membership: {
    premium: '#FFB300',                             // 金色 - 高級會員
    expired: isDark ? '#FF9800' : '#F57C00',        // 橙色 - 已過期 (更協調的暖色調)
    trial: isDark ? '#66BB6A' : '#4CAF50',          // 綠色 - 試用狀態
    regular: isDark ? '#26C6DA' : '#00838F',        // 青色 - 普通會員
  },

  // AppBar 組件專用顏色
  appBar: {
    // 會員狀態按鈕顏色
    membershipButton: {
      trial: {
        primary: isDark ? '#66BB6A' : '#4CAF50',
        secondary: isDark ? '#4CAF50' : '#388E3C',
        shadow: isDark ? 'rgba(102, 187, 106, 0.3)' : 'rgba(76, 175, 80, 0.3)',
        hoverShadow: isDark ? 'rgba(102, 187, 106, 0.5)' : 'rgba(76, 175, 80, 0.5)',
      },
      expired: {
        primary: isDark ? '#FF9800' : '#F57C00',
        secondary: isDark ? '#F57C00' : '#EF6C00',
        shadow: isDark ? 'rgba(255, 152, 0, 0.3)' : 'rgba(245, 124, 0, 0.3)',
        hoverShadow: isDark ? 'rgba(255, 152, 0, 0.5)' : 'rgba(245, 124, 0, 0.5)',
      },
      premium: {
        primary: '#FFD700',
        secondary: '#FFB300',
        shadow: 'rgba(255, 215, 0, 0.3)',
        hoverShadow: 'rgba(255, 215, 0, 0.5)',
      },
    },
    // 刷新認證按鈕顏色
    refreshButton: {
      borderColor: isDark ? 'rgba(255, 255, 255, 0.3)' : 'rgba(255, 255, 255, 0.6)',
      backgroundColor: isDark ? 'rgba(255, 255, 255, 0.05)' : 'rgba(255, 255, 255, 0.15)',
      color: isDark ? '#FAFAFA' : '#FFFFFF',
      hover: {
        borderColor: isDark ? 'rgba(255, 255, 255, 0.5)' : 'rgba(255, 255, 255, 0.9)',
        backgroundColor: isDark ? 'rgba(255, 255, 255, 0.1)' : 'rgba(255, 255, 255, 0.25)',
        boxShadow: isDark ? '0 4px 12px rgba(255, 255, 255, 0.1)' : '0 4px 12px rgba(0, 0, 0, 0.15)',
      },
      disabled: {
        borderColor: isDark ? 'rgba(255, 255, 255, 0.1)' : 'rgba(255, 255, 255, 0.3)',
        backgroundColor: isDark ? 'rgba(255, 255, 255, 0.02)' : 'rgba(255, 255, 255, 0.05)',
        color: isDark ? 'rgba(255, 255, 255, 0.3)' : 'rgba(255, 255, 255, 0.5)',
      },
    },
  },
  
  // 數據使用量指示器顏色
  dataUsage: (percentage: number) => {
    if (percentage < 0.7) return isDark ? '#66BB6A' : '#2E7D32';      // 綠色 - 正常
    if (percentage < 0.9) return isDark ? '#FFB74D' : '#F57C00';      // 橙色 - 警告
    return isDark ? '#EF5350' : '#C62828';                             // 紅色 - 超限
  },
});

// 为了向后兼容，导出默认主题
export const theme = lightTheme;
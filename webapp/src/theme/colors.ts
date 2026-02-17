/**
 * Centralized Theme Color Configuration
 *
 * Network & Security Theme
 * - Easy to customize from one location
 * - Consistent across all pages
 * - Supports light and dark modes
 */

export const APP_COLORS = {
  light: {
    // Primary gradient background
    bgGradient: "linear-gradient(135deg, #0f2027 0%, #203a43 50%, #2c5364 100%)",

    // Accent colors - Electric Cyan
    accent: "#00d4ff",
    accentLight: "#33ddff",
    accentDark: "#0099cc",
    accentGlow: "rgba(0, 212, 255, 0.3)",

    // Card backgrounds - Frosted glass
    cardBg: "rgba(255, 255, 255, 0.95)",
    cardBgHover: "rgba(255, 255, 255, 0.98)",
    cardBorder: "rgba(0, 0, 0, 0.06)",
    cardBorderLight: "rgba(255, 255, 255, 0.3)",
    cardGradient: "linear-gradient(135deg, rgba(255, 255, 255, 0.95) 0%, rgba(248, 249, 250, 0.95) 100%)",
    cardShadow: "0 4px 12px rgba(0, 0, 0, 0.1)",
    cardShadowHover: "0 8px 24px rgba(0, 0, 0, 0.15)",

    // Status colors with gradients and enhanced glows
    success: "#4caf50",
    successLight: "#66bb6a",
    successDark: "#388E3C",
    successGlow: "rgba(76, 175, 80, 0.3)",
    successGlowStrong: "rgba(76, 175, 80, 0.5)",
    successGradient: "linear-gradient(135deg, #4CAF50 0%, #388E3C 100%)",
    successBgLight: "rgba(76, 175, 80, 0.08)",
    successBgLighter: "rgba(76, 175, 80, 0.04)",
    successBorder: "rgba(76, 175, 80, 0.2)",

    warning: "#ff9800",
    warningLight: "#ffa726",
    warningDark: "#F57C00",
    warningGlow: "rgba(255, 152, 0, 0.3)",
    warningGlowStrong: "rgba(255, 152, 0, 0.5)",
    warningGradient: "linear-gradient(135deg, #FF9800 0%, #F57C00 100%)",
    warningBgLight: "rgba(255, 152, 0, 0.08)",
    warningBgLighter: "rgba(255, 152, 0, 0.04)",
    warningBorder: "rgba(255, 152, 0, 0.2)",

    error: "#f44336",
    errorLight: "#ef5350",
    errorDark: "#d32f2f",
    errorGlow: "rgba(244, 67, 54, 0.3)",
    errorGlowStrong: "rgba(244, 67, 54, 0.5)",
    errorGradient: "linear-gradient(135deg, #f44336 0%, #d32f2f 100%)",
    errorBgLight: "rgba(244, 67, 54, 0.08)",
    errorBgLighter: "rgba(244, 67, 54, 0.04)",
    errorBorder: "rgba(244, 67, 54, 0.2)",

    info: "#2196f3",
    infoLight: "#42a5f5",
    infoDark: "#1565C0",
    infoGlow: "rgba(33, 150, 243, 0.3)",
    infoGlowStrong: "rgba(33, 150, 243, 0.5)",
    infoGradient: "linear-gradient(135deg, #2196F3 0%, #1565C0 100%)",
    infoBgLight: "rgba(33, 150, 243, 0.08)",
    infoBgLighter: "rgba(33, 150, 243, 0.04)",
    infoBorder: "rgba(33, 150, 243, 0.2)",

    // Selection and highlight colors
    selectedBg: "rgba(33, 150, 243, 0.1)",
    selectedBgHover: "rgba(33, 150, 243, 0.15)",
    selectedGradient: "linear-gradient(135deg, rgba(25, 118, 210, 0.08) 0%, rgba(66, 165, 245, 0.04) 100%)",
    selectedBorder: "rgba(33, 150, 243, 0.3)",
    selectedShadow: "0 8px 24px rgba(25, 118, 210, 0.25)",

    // Disabled state
    disabled: "#BDBDBD",
    disabledGradient: "linear-gradient(135deg, #BDBDBD 0%, #9E9E9E 100%)",
    disabledBg: "rgba(0, 0, 0, 0.04)",
    disabledShadow: "0 10px 30px rgba(0, 0, 0, 0.2)",

    // Overlay and glass effects
    overlay: "linear-gradient(135deg, rgba(255,255,255,0.3) 0%, rgba(255,255,255,0) 100%)",
    glassBg: "rgba(255, 255, 255, 0.1)",
    glassBorder: "rgba(255, 255, 255, 0.2)",

    // Network animation elements
    gridColor: "rgba(0, 212, 255, 0.15)",
    particleColor: "rgba(0, 212, 255, 0.6)",

    // Text colors
    textPrimary: "rgba(0, 0, 0, 0.87)",
    textSecondary: "rgba(0, 0, 0, 0.6)",
    textDisabled: "rgba(0, 0, 0, 0.38)",

    // Progress and dividers
    progressBg: "rgba(0, 0, 0, 0.1)",
    divider: "rgba(0, 0, 0, 0.12)",

    // Highlighted text
    highlightColor: "rgba(25, 118, 210, 1)",
    highlightBg: "rgba(25, 118, 210, 0.08)",
  },

  dark: {
    // Primary gradient background
    bgGradient: "linear-gradient(135deg, #0a0e27 0%, #1a1f3a 50%, #2d1b4e 100%)",

    // Accent colors - Neon Cyan
    accent: "#00ffff",
    accentLight: "#33ffff",
    accentDark: "#00cccc",
    accentGlow: "rgba(0, 255, 255, 0.4)",

    // Card backgrounds - Dark frosted glass
    cardBg: "rgba(20, 25, 45, 0.9)",
    cardBgHover: "rgba(30, 35, 55, 0.95)",
    cardBorder: "rgba(255, 255, 255, 0.12)",
    cardBorderLight: "rgba(100, 150, 255, 0.2)",
    cardGradient: "linear-gradient(135deg, rgba(33, 33, 33, 0.95) 0%, rgba(28, 28, 28, 0.95) 100%)",
    cardShadow: "0 4px 12px rgba(0, 0, 0, 0.3)",
    cardShadowHover: "0 8px 24px rgba(0, 0, 0, 0.4)",

    // Status colors with gradients and enhanced glows (adjusted for dark mode)
    success: "#66bb6a",
    successLight: "#81c784",
    successDark: "#4caf50",
    successGlow: "rgba(102, 187, 106, 0.3)",
    successGlowStrong: "rgba(102, 187, 106, 0.5)",
    successGradient: "linear-gradient(135deg, #66bb6a 0%, #4caf50 100%)",
    successBgLight: "rgba(102, 187, 106, 0.15)",
    successBgLighter: "rgba(102, 187, 106, 0.08)",
    successBorder: "rgba(102, 187, 106, 0.3)",

    warning: "#ffa726",
    warningLight: "#ffb74d",
    warningDark: "#ff9800",
    warningGlow: "rgba(255, 167, 38, 0.3)",
    warningGlowStrong: "rgba(255, 167, 38, 0.5)",
    warningGradient: "linear-gradient(135deg, #ffa726 0%, #ff9800 100%)",
    warningBgLight: "rgba(255, 167, 38, 0.15)",
    warningBgLighter: "rgba(255, 167, 38, 0.08)",
    warningBorder: "rgba(255, 167, 38, 0.3)",

    error: "#ef5350",
    errorLight: "#e57373",
    errorDark: "#f44336",
    errorGlow: "rgba(239, 83, 80, 0.3)",
    errorGlowStrong: "rgba(239, 83, 80, 0.5)",
    errorGradient: "linear-gradient(135deg, #ef5350 0%, #f44336 100%)",
    errorBgLight: "rgba(239, 83, 80, 0.15)",
    errorBgLighter: "rgba(239, 83, 80, 0.08)",
    errorBorder: "rgba(239, 83, 80, 0.3)",

    info: "#42a5f5",
    infoLight: "#64b5f6",
    infoDark: "#2196f3",
    infoGlow: "rgba(66, 165, 245, 0.3)",
    infoGlowStrong: "rgba(66, 165, 245, 0.5)",
    infoGradient: "linear-gradient(135deg, #42a5f5 0%, #2196f3 100%)",
    infoBgLight: "rgba(66, 165, 245, 0.15)",
    infoBgLighter: "rgba(66, 165, 245, 0.1)",
    infoBorder: "rgba(66, 165, 245, 0.3)",

    // Selection and highlight colors
    selectedBg: "rgba(66, 165, 245, 0.15)",
    selectedBgHover: "rgba(66, 165, 245, 0.2)",
    selectedGradient: "linear-gradient(135deg, rgba(66, 165, 245, 0.2) 0%, rgba(33, 150, 243, 0.15) 100%)",
    selectedBorder: "rgba(66, 165, 245, 0.4)",
    selectedShadow: "0 8px 24px rgba(33, 150, 243, 0.3)",

    // Disabled state
    disabled: "#757575",
    disabledGradient: "linear-gradient(135deg, #757575 0%, #616161 100%)",
    disabledBg: "rgba(255, 255, 255, 0.08)",
    disabledShadow: "0 10px 30px rgba(0, 0, 0, 0.3)",

    // Overlay and glass effects
    overlay: "linear-gradient(135deg, rgba(255,255,255,0.1) 0%, rgba(255,255,255,0) 100%)",
    glassBg: "rgba(255, 255, 255, 0.05)",
    glassBorder: "rgba(255, 255, 255, 0.1)",

    // Network animation elements
    gridColor: "rgba(0, 255, 255, 0.15)",
    particleColor: "rgba(0, 255, 255, 0.7)",

    // Text colors
    textPrimary: "rgba(255, 255, 255, 0.95)",
    textSecondary: "rgba(255, 255, 255, 0.7)",
    textDisabled: "rgba(255, 255, 255, 0.5)",

    // Progress and dividers
    progressBg: "rgba(255, 255, 255, 0.1)",
    divider: "rgba(255, 255, 255, 0.12)",

    // Highlighted text
    highlightColor: "rgba(144, 202, 249, 1)",
    highlightBg: "rgba(144, 202, 249, 0.16)",
  },
};

/**
 * Helper function to get colors based on current theme mode
 */
export const getThemeColors = (isDark: boolean) => {
  return isDark ? APP_COLORS.dark : APP_COLORS.light;
};

/**
 * Helper to get status-based box shadow for animations
 */
export const getStatusShadow = (status: 'connected' | 'transitioning' | 'disconnected' | 'disabled' | 'stop', isDark: boolean) => {
  const colors = getThemeColors(isDark);

  switch (status) {
    case 'connected':
      return `0 20px 60px ${colors.successGlow}, 0 0 0 0 ${colors.successGlowStrong}`;
    case 'transitioning':
      return `0 20px 60px ${colors.warningGlow}, 0 0 0 0 ${colors.warningGlowStrong}`;
    case 'disconnected':
      return `0 20px 60px ${colors.infoGlow}, 0 0 0 0 ${colors.infoGlowStrong}`;
    case 'stop':
      return `0 20px 60px ${colors.errorGlow}, 0 0 0 0 ${colors.errorGlowStrong}`;
    case 'disabled':
      return colors.disabledShadow;
    default:
      return 'none';
  }
};

/**
 * Helper to get status-based gradient
 */
export const getStatusGradient = (status: 'connected' | 'transitioning' | 'disconnected' | 'disabled' | 'stop', isDark: boolean) => {
  const colors = getThemeColors(isDark);

  switch (status) {
    case 'connected':
      return colors.successGradient;
    case 'transitioning':
      return colors.warningGradient;
    case 'disconnected':
      return colors.infoGradient;
    case 'stop':
      return colors.errorGradient;
    case 'disabled':
      return colors.disabledGradient;
    default:
      return colors.infoGradient;
  }
};

/**
 * Helper to get status-based color
 */
export const getStatusColor = (status: 'connected' | 'transitioning' | 'disconnected' | 'disabled', isDark: boolean) => {
  const colors = getThemeColors(isDark);

  switch (status) {
    case 'connected':
      return colors.success;
    case 'transitioning':
      return colors.warning;
    case 'disconnected':
      return colors.disabled;
    case 'disabled':
      return colors.disabled;
    default:
      return colors.textSecondary;
  }
};

/**
 * Common animation keyframes
 */
export const ANIMATIONS = `
  @keyframes networkGrid {
    0%, 100% {
      transform: translateY(0) translateX(0);
      opacity: 0.3;
    }
    50% {
      transform: translateY(-20px) translateX(10px);
      opacity: 0.6;
    }
  }

  @keyframes dataFlow {
    0% {
      transform: translateY(-100%) scale(0);
      opacity: 0;
    }
    50% {
      opacity: 1;
    }
    100% {
      transform: translateY(100vh) scale(1);
      opacity: 0;
    }
  }

  @keyframes pulse {
    0%, 100% {
      transform: scale(1);
      opacity: 0.5;
    }
    50% {
      transform: scale(1.5);
      opacity: 0.8;
    }
  }

  @keyframes gradientShift {
    0% {
      background-position: 0% 50%;
    }
    50% {
      background-position: 100% 50%;
    }
    100% {
      background-position: 0% 50%;
    }
  }

  @keyframes fadeInUp {
    from {
      opacity: 0;
      transform: translateY(30px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  @keyframes fadeIn {
    from {
      opacity: 0;
    }
    to {
      opacity: 1;
    }
  }

  @keyframes shimmer {
    0% {
      background-position: -200% center;
    }
    100% {
      background-position: 200% center;
    }
  }

  @keyframes slideInRight {
    from {
      transform: translateX(100%);
      opacity: 0;
    }
    to {
      transform: translateX(0);
      opacity: 1;
    }
  }

  @keyframes slideInLeft {
    from {
      transform: translateX(-100%);
      opacity: 0;
    }
    to {
      transform: translateX(0);
      opacity: 1;
    }
  }

  @keyframes scaleIn {
    from {
      transform: scale(0.8);
      opacity: 0;
    }
    to {
      transform: scale(1);
      opacity: 1;
    }
  }
`;

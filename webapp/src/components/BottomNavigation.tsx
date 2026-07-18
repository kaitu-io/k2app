import { styled, keyframes } from "@mui/material/styles";
import {
  BottomNavigation as MuiBottomNavigation,
  BottomNavigationAction,
  Paper,
} from "@mui/material";
import {
  Dashboard as DashboardIcon,
  ShoppingCart as PurchaseIcon,
  CardGiftcard as InviteIcon,
  Explore as DiscoverIcon,
  AccountCircle as AccountIcon,
  Router as RouterIcon,
} from "@mui/icons-material";
import { useNavigate, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { getCurrentAppConfig } from "../config/apps";
import { useMemo, memo } from "react";
import { useUser } from "../hooks/useUser";
import { useAuthStore } from "../stores";
import { useRouterStore } from "../stores/router.store";

const inviteWiggle = keyframes`
  0%, 82%, 100% { transform: rotate(0deg) scale(1); }
  85% { transform: rotate(-20deg) scale(1.15); }
  88% { transform: rotate(18deg) scale(1.18); }
  91% { transform: rotate(-14deg) scale(1.12); }
  94% { transform: rotate(10deg) scale(1.06); }
  97% { transform: rotate(-5deg) scale(1.02); }
`;

const inviteGlow = keyframes`
  0%, 80%, 100% { filter: drop-shadow(0 0 0px transparent); color: inherit; }
  86%, 92% { filter: drop-shadow(0 0 8px rgba(255, 167, 38, 0.85)); color: #ffa726; }
`;

const dotPulse = keyframes`
  0%, 100% { transform: scale(1); opacity: 1; }
  50% { transform: scale(1.4); opacity: 0.65; }
`;

const AnimatedInviteIcon = styled("span")(() => ({
  display: "inline-flex",
  position: "relative",
  transformOrigin: "center 70%",
  animation: `${inviteWiggle} 6s ease-in-out infinite, ${inviteGlow} 6s ease-in-out infinite`,
  "&::after": {
    content: '""',
    position: "absolute",
    top: 1,
    right: -1,
    width: 7,
    height: 7,
    borderRadius: "50%",
    backgroundColor: "#ffa726",
    boxShadow: "0 0 5px rgba(255, 167, 38, 0.7)",
    animation: `${dotPulse} 2s ease-in-out infinite`,
  },
}));

const StyledPaper = styled(Paper)(({ theme }) => ({
  // Use relative positioning - parent controls placement via flexbox
  position: "relative",
  zIndex: theme.zIndex.appBar,
  borderTop: `1px solid ${
    theme.palette.mode === "dark"
      ? "rgba(255, 255, 255, 0.12)"
      : "rgba(0, 0, 0, 0.12)"
  }`,
  // 为底部安全区域（如 iPhone 底部横条、Android 手势导航）留出空间
  paddingBottom: 'env(safe-area-inset-bottom, 0px)',
}));

const StyledBottomNavigation = styled(MuiBottomNavigation)(({ theme }) => ({
  height: 56,
  backgroundColor:
    theme.palette.mode === "dark"
      ? theme.palette.background.paper
      : theme.palette.background.default,
}));

const StyledBottomNavigationAction = styled(BottomNavigationAction)(
  ({ theme }) => ({
    minWidth: 60,
    maxWidth: 100,
    color: theme.palette.text.secondary,
    "&.Mui-selected": {
      color: theme.palette.primary.main,
      "& .MuiBottomNavigationAction-label": {
        fontSize: "0.75rem",
        fontWeight: 600,
      },
    },
    "& .MuiBottomNavigationAction-label": {
      fontSize: "0.7rem",
      marginTop: theme.spacing(0.5),
    },
  })
);

function BottomNavigation() {
  const navigate = useNavigate();
  const location = useLocation();
  const { t } = useTranslation();
  const appConfig = getCurrentAppConfig();
  const { user } = useUser();
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  // Router tab appears once a router has ever been seen (phase !== 'none') —
  // subscribed live so the entry reacts to discovery/unbind without a reload.
  const hasRouter = useRouterStore((s) => s.phase !== 'none');

  // Define all navigation items
  const allNavItems = useMemo(() => {
    const items = [
      {
        label: t("nav:navigation.dashboard"),
        icon: <DashboardIcon />,
        path: "/",
        feature: null,
      },
      // Router tab — appears once a router has ever been paired (see hasRouter above)
      ...(hasRouter ? [{
        label: t("nav:navigation.router"),
        icon: <RouterIcon />,
        path: "/router",
        feature: null,
      }] : []),
      {
        label: isAuthenticated ? t("nav:navigation.purchase") : t("nav:navigation.activate"),
        icon: <PurchaseIcon />,
        path: "/purchase",
        feature: null,
      },
      {
        label: user?.isRetailer ? t("nav:navigation.retailer") : t("nav:navigation.invite"),
        icon: <AnimatedInviteIcon><InviteIcon /></AnimatedInviteIcon>,
        path: "/invite",
        feature: "invite" as const,
      },
      {
        label: t("nav:navigation.discover"),
        icon: <DiscoverIcon />,
        path: "/discover",
        feature: "discover" as const,
      },
      {
        label: t("nav:navigation.account"),
        icon: <AccountIcon />,
        path: "/account",
        feature: null,
      },
    ];

    // Filter based on feature flags
    const filtered = items.filter(
      (item) => item.feature === null || appConfig.features[item.feature]
    );

    // iOS: 仅当原生 StoreKit IAP 能力缺失时隐藏 purchase 入口。
    // IAP 已注入（capacitor-k2 在 iOS 注入 _platform.iap）→ 显示入口，
    // Purchase 页走 IAP 内联面板，绝不开外链，满足 Apple 3.1.1。
    if (window._platform?.os === 'ios' && !window._platform?.iap) {
      return filtered.filter(item => item.path !== '/purchase');
    }

    return filtered;
  }, [t, appConfig.features, user?.isRetailer, isAuthenticated, hasRouter]);

  // Get current active path
  const currentPath = location.pathname;

  // Memoize navigation handler to avoid recreating on every render
  const handleNavigationChange = useMemo(() => {
    return (_: any, newValue: string) => {
      navigate(newValue);
    };
  }, [navigate]);

  return (
    <StyledPaper elevation={8}>
      <StyledBottomNavigation
        value={currentPath}
        onChange={handleNavigationChange}
        showLabels
      >
        {allNavItems.map((item) => (
          <StyledBottomNavigationAction
            key={item.path}
            label={item.label}
            icon={item.icon}
            value={item.path}
          />
        ))}
      </StyledBottomNavigation>
    </StyledPaper>
  );
}

// Export memoized component to prevent unnecessary re-renders
export default memo(BottomNavigation);

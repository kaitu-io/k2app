import { styled } from "@mui/material/styles";
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
} from "@mui/icons-material";
import { useNavigate, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { getCurrentAppConfig } from "../config/apps";
import { useMemo, memo } from "react";
import { useUser } from "../hooks/useUser";
import { useAuthStore } from "../stores";

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

  // Define all navigation items
  const allNavItems = useMemo(() => {
    const items = [
      {
        label: t("nav:navigation.dashboard"),
        icon: <DashboardIcon />,
        path: "/",
        feature: null,
      },
      {
        label: isAuthenticated ? t("nav:navigation.purchase") : t("nav:navigation.activate"),
        icon: <PurchaseIcon />,
        path: "/purchase",
        feature: null,
      },
      {
        label: user?.isRetailer ? t("nav:navigation.retailer") : t("nav:navigation.invite"),
        icon: <InviteIcon />,
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
    return items.filter(
      (item) => item.feature === null || appConfig.features[item.feature]
    );
  }, [t, appConfig.features, user?.isRetailer, isAuthenticated]);

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

/**
 * Side Navigation Component
 *
 * Desktop sidebar navigation for wider screens and OpenWRT router mode.
 * Uses the same navigation items and feature flags as BottomNavigation.
 */

import { styled } from "@mui/material/styles";
import {
  Drawer,
  List,
  ListItem,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Box,
  Typography,
  Divider,
} from "@mui/material";
import {
  Dashboard as DashboardIcon,
  ShoppingCart as PurchaseIcon,
  CardGiftcard as InviteIcon,
  Explore as DiscoverIcon,
  AccountCircle as AccountIcon,
  Devices as DevicesIcon,
  History as HistoryIcon,
  HelpOutline as HelpIcon,
} from "@mui/icons-material";
import { useNavigate, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { getCurrentAppConfig } from "../config/apps";
import { useMemo, memo } from "react";
import { useUser } from "../hooks/useUser";
const SIDEBAR_WIDTH = 220;

const StyledDrawer = styled(Drawer)(({ theme }) => ({
  width: SIDEBAR_WIDTH,
  flexShrink: 0,
  "& .MuiDrawer-paper": {
    width: SIDEBAR_WIDTH,
    boxSizing: "border-box",
    borderRight: `1px solid ${
      theme.palette.mode === "dark"
        ? "rgba(255, 255, 255, 0.12)"
        : "rgba(0, 0, 0, 0.12)"
    }`,
    backgroundColor:
      theme.palette.mode === "dark"
        ? theme.palette.background.paper
        : theme.palette.background.default,
  },
}));

const Logo = styled(Box)(({ theme }) => ({
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  padding: theme.spacing(2),
  height: 64,
}));

const StyledListItemButton = styled(ListItemButton)(({ theme }) => ({
  borderRadius: theme.shape.borderRadius,
  margin: theme.spacing(0.5, 1),
  "&.Mui-selected": {
    backgroundColor:
      theme.palette.mode === "dark"
        ? "rgba(255, 255, 255, 0.08)"
        : "rgba(0, 0, 0, 0.04)",
    "& .MuiListItemIcon-root": {
      color: theme.palette.primary.main,
    },
    "& .MuiListItemText-primary": {
      color: theme.palette.primary.main,
      fontWeight: 600,
    },
  },
  "&:hover": {
    backgroundColor:
      theme.palette.mode === "dark"
        ? "rgba(255, 255, 255, 0.04)"
        : "rgba(0, 0, 0, 0.02)",
  },
}));

interface NavItem {
  label: string;
  icon: React.ReactNode;
  path: string;
  feature: string | null;
  secondary?: boolean; // For secondary navigation items
}

function SideNavigation() {
  const navigate = useNavigate();
  const location = useLocation();
  const { t } = useTranslation();
  const appConfig = getCurrentAppConfig();
  const { user } = useUser();

  // Primary navigation items (same as bottom navigation)
  const primaryNavItems = useMemo<NavItem[]>(() => {
    const items: NavItem[] = [
      {
        label: t("nav:navigation.dashboard"),
        icon: <DashboardIcon />,
        path: "/",
        feature: null,
      },
      {
        label: t("nav:navigation.purchase"),
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

    return items.filter(
      (item) => item.feature === null || (appConfig.features as Record<string, unknown>)[item.feature]
    );
  }, [t, appConfig.features, user?.isRetailer]);

  // Secondary navigation items (additional items not shown in bottom nav)
  const secondaryNavItems = useMemo<NavItem[]>(() => {
    const items: NavItem[] = [
      {
        label: t("nav:navigation.devices"),
        icon: <DevicesIcon />,
        path: "/devices",
        feature: null,
        secondary: true,
      },
      {
        label: t("nav:navigation.proHistory"),
        icon: <HistoryIcon />,
        path: "/pro-histories",
        feature: "proHistory" as const,
        secondary: true,
      },
      {
        label: t("nav:navigation.help"),
        icon: <HelpIcon />,
        path: "/faq",
        feature: "feedback" as const,
        secondary: true,
      },
    ];

    return items.filter(
      (item) => item.feature === null || (appConfig.features as Record<string, unknown>)[item.feature]
    );
  }, [t, appConfig.features]);

  const handleNavigation = (path: string) => {
    navigate(path);
  };

  // Check if current path matches nav item
  const isSelected = (path: string) => {
    if (path === "/") {
      return location.pathname === "/";
    }
    return location.pathname.startsWith(path);
  };

  return (
    <StyledDrawer variant="permanent" anchor="left">
      {/* Logo / App Name */}
      <Logo>
        <Typography
          variant="h6"
          sx={{
            fontWeight: 700,
            color: "primary.main",
            letterSpacing: "0.5px",
          }}
        >
          {appConfig.appName}
        </Typography>
      </Logo>

      <Divider />

      {/* Primary Navigation */}
      <List sx={{ flexGrow: 1, pt: 1 }}>
        {primaryNavItems.map((item) => (
          <ListItem key={item.path} disablePadding>
            <StyledListItemButton
              selected={isSelected(item.path)}
              onClick={() => handleNavigation(item.path)}
            >
              <ListItemIcon sx={{ minWidth: 40 }}>{item.icon}</ListItemIcon>
              <ListItemText
                primary={item.label}
                primaryTypographyProps={{
                  fontSize: "0.875rem",
                }}
              />
            </StyledListItemButton>
          </ListItem>
        ))}
      </List>

      {/* Secondary Navigation */}
      {secondaryNavItems.length > 0 && (
        <>
          <Divider />
          <List sx={{ pt: 1 }}>
            {secondaryNavItems.map((item) => (
              <ListItem key={item.path} disablePadding>
                <StyledListItemButton
                  selected={isSelected(item.path)}
                  onClick={() => handleNavigation(item.path)}
                >
                  <ListItemIcon sx={{ minWidth: 40 }}>{item.icon}</ListItemIcon>
                  <ListItemText
                    primary={item.label}
                    primaryTypographyProps={{
                      fontSize: "0.875rem",
                    }}
                  />
                </StyledListItemButton>
              </ListItem>
            ))}
          </List>
        </>
      )}

      {/* Bottom padding */}
      <Box sx={{ height: 16 }} />
    </StyledDrawer>
  );
}

export default memo(SideNavigation);

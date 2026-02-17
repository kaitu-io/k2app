import { styled, useTheme } from "@mui/material/styles";
import { Box } from "@mui/material";
import { useLocation, Outlet } from "react-router-dom";
import { useState, useEffect, lazy, Suspense } from "react";
import BottomNavigation from "./BottomNavigation";
import SideNavigation from "./SideNavigation";
import AnnouncementBanner from "./AnnouncementBanner";
import ServiceAlert from "./ServiceAlert";
import FeedbackButton from "./FeedbackButton";
import { useLayout } from "../stores";
import { getCurrentAppConfig } from "../config/apps";
import LoginRequiredGuard from "./LoginRequiredGuard";

// Lazy load Tab pages for code splitting, but keep them mounted once loaded
const Dashboard = lazy(() => import("../pages/Dashboard"));
// Purchase 移出 keep-alive，在 App.tsx 中作为普通路由
const InviteHub = lazy(() => import("../pages/InviteHub"));
const Discover = lazy(() => import("../pages/Discover"));
const Account = lazy(() => import("../pages/Account"));

const SIDEBAR_WIDTH = 220;

interface MainProps {
  isDesktop?: boolean;
}

// CSS 变量：移动端顶部安全区域高度
const SAFE_AREA_TOP = 'env(safe-area-inset-top, 0px)';

const Main = styled("main", {
  shouldForwardProp: (prop) => prop !== 'isDesktop',
})<MainProps>(({ theme, isDesktop }) => ({
  // Flex child: fill remaining space
  flex: 1,
  // Critical: allow flex child to shrink below content size
  minHeight: 0,
  display: 'flex',
  flexDirection: 'column',
  padding: 0,
  position: "relative",
  backgroundColor: theme.palette.mode === 'dark'
    ? theme.palette.grey[900]
    : theme.palette.grey[100],
  // Desktop: add left margin for sidebar
  marginLeft: isDesktop ? SIDEBAR_WIDTH : 0,
  transition: theme.transitions.create(['margin-left'], {
    duration: theme.transitions.duration.standard,
  }),
}));

// Define Tab pages configuration
interface TabPageConfig {
  path: string;
  component: React.LazyExoticComponent<() => JSX.Element>;
  requiresLogin?: boolean;
  noPadding?: boolean; // For full-screen pages like Discover
  featureFlag?: 'invite' | 'discover' | 'proHistory' | 'feedback' | 'deviceInstall' | 'memberManagement' | 'updateLoginEmail';
}

const TAB_PAGES: TabPageConfig[] = [
  { path: '/', component: Dashboard, noPadding: true },
  // Purchase 移出 keep-alive，改为普通路由（避免与 LoginRequiredGuard 冲突）
  { path: '/invite', component: InviteHub, requiresLogin: true, featureFlag: 'invite' },
  { path: '/discover', component: Discover, noPadding: true, featureFlag: 'discover' },
  { path: '/account', component: Account }, // 允许未登录查看，MembershipCard 自行处理登录状态
];

export default function Layout() {
  const location = useLocation();
  const { isDesktop } = useLayout();
  const appConfig = getCurrentAppConfig();
  const theme = useTheme();

  // Track which Tab pages have been mounted (for lazy loading and keep-alive)
  const [mountedTabs, setMountedTabs] = useState<Record<string, boolean>>({});

  // Check if current path is a Tab page
  const currentTabPage = TAB_PAGES.find(tab => tab.path === location.pathname);
  const isTabPage = !!currentTabPage;

  // Mount Tab page on first visit
  useEffect(() => {
    if (currentTabPage && !mountedTabs[currentTabPage.path]) {
      // Check if feature is enabled (if feature flag exists)
      const featureEnabled = !currentTabPage.featureFlag ||
        appConfig.features[currentTabPage.featureFlag];

      if (featureEnabled) {
        setMountedTabs(prev => ({ ...prev, [currentTabPage.path]: true }));
      }
    }
  }, [currentTabPage, mountedTabs, appConfig.features]);

  return (
    <Box sx={{
      display: "flex",
      flexDirection: "column",
      height: "100%",
      boxSizing: "border-box",
      overflow: "hidden",
      // Mobile: 整个布局容器添加顶部 safe-area padding
      // 这样状态栏区域始终有背景色，不会看到透明
      paddingTop: isDesktop ? 0 : SAFE_AREA_TOP,
      // 背景色：确保状态栏区域有正确的背景色
      backgroundColor: theme.palette.mode === 'dark'
        ? theme.palette.grey[900]
        : theme.palette.grey[100],
    }}>
      {/* Announcement Banner - shown at top, fixed height */}
      <Box sx={{ flexShrink: 0, marginLeft: isDesktop ? `${SIDEBAR_WIDTH}px` : 0 }}>
        <AnnouncementBanner />
      </Box>

      {/* Global Service Alert - Fixed at top, shows service failure or network errors */}
      <ServiceAlert sidebarWidth={isDesktop ? SIDEBAR_WIDTH : 0} />

      {/* Desktop: Side Navigation */}
      {isDesktop && <SideNavigation />}

      <Main isDesktop={isDesktop}>
        {/* Tab Pages - Keep Alive (cached, hidden when not active) */}
        {TAB_PAGES.map((tabPage) => {
          const isMounted = mountedTabs[tabPage.path];
          const isActive = location.pathname === tabPage.path;
          const Component = tabPage.component;

          // Check if feature is enabled
          const featureEnabled = !tabPage.featureFlag ||
            appConfig.features[tabPage.featureFlag];

          if (!featureEnabled || !isMounted) {
            return null;
          }

          // Wrap with LoginRequiredGuard if needed
          const content = (
            <Suspense fallback={null}>
              <Component />
            </Suspense>
          );

          const guardedContent = tabPage.requiresLogin ? (
            <LoginRequiredGuard pagePath={tabPage.path}>{content}</LoginRequiredGuard>
          ) : content;

          return (
            <Box
              key={tabPage.path}
              sx={{
                // Use flex layout instead of absolute positioning
                flex: 1,
                minHeight: 0,
                overflow: "auto",
                // Apply padding for normal pages, no padding for full-screen pages (like Discover)
                padding: tabPage.noPadding ? 0 : 2,
                paddingBottom: tabPage.noPadding ? 0 : 0.5,
                // Use visibility to keep component state while hidden
                visibility: isActive ? 'visible' : 'hidden',
                // Prevent interaction when hidden
                pointerEvents: isActive ? 'auto' : 'none',
                // When hidden, collapse to 0 height so other content can take space
                ...(isActive ? {} : { position: 'absolute', height: 0, width: '100%' }),
              }}
            >
              {guardedContent}
            </Box>
          );
        })}

        {/* Other Pages (non-Tab) - Normal routing */}
        {!isTabPage && (
          <Box
            sx={{
              // Use flex layout: fill remaining space, scroll internally
              flex: 1,
              minHeight: 0,
              overflow: "auto",
              padding: 2,
              paddingBottom: 0.5,
            }}
          >
            <Outlet />
          </Box>
        )}
      </Main>

      {/* Mobile: Bottom Navigation - fixed at bottom, outside scroll area */}
      {!isDesktop && (
        <Box sx={{ flexShrink: 0 }}>
          <BottomNavigation />
        </Box>
      )}

      {/* Floating Feedback Button - draggable, for log collection */}
      <FeedbackButton />
    </Box>
  );
}

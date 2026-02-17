/**
 * Tunnels - 自部署节点管理页面
 *
 * 功能说明：
 * - 该功能正在开发中，即将推出
 * - 目前显示"敬请期待"状态
 * - 引导用户登录使用云端节点
 */

import {
  Box,
  Typography,
  Button,
  Stack,
  Paper,
  useTheme,
} from "@mui/material";
import {
  Cloud as CloudIcon,
  Code as CodeIcon,
  Login as LoginIcon,
} from "@mui/icons-material";
import BackButton from "../components/BackButton";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "../stores";
import { useLoginDialogStore } from "../stores/login-dialog.store";
import { getThemeColors } from "../theme/colors";

export default function Tunnels() {
  const { t } = useTranslation("dashboard");
  const theme = useTheme();
  const isDark = theme.palette.mode === "dark";
  const colors = getThemeColors(isDark);

  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const openLoginDialog = useLoginDialogStore((s) => s.open);

  const handleLogin = () => {
    openLoginDialog({
      trigger: "tunnels-page",
      message: t("tunnels.loginToSync", "登录后可获取云端节点"),
    });
  };

  return (
    <Box sx={{ p: 2, pb: 6 }}>
      {/* Header */}
      <Box sx={{ display: "flex", alignItems: "center", mb: 2 }}>
        <BackButton />
        <Typography variant="h6" fontWeight={600} sx={{ ml: 1 }}>
          {t("tunnels.title", "节点管理")}
        </Typography>
      </Box>

      {/* Self-Deploy Feature Card */}
      <Paper
        elevation={0}
        sx={{
          p: 2.5,
          borderRadius: 2,
          textAlign: 'center',
          bgcolor: isDark ? 'rgba(0, 212, 255, 0.05)' : 'rgba(0, 150, 255, 0.04)',
          border: `1px solid ${isDark ? 'rgba(0, 212, 255, 0.2)' : 'rgba(0, 150, 255, 0.15)'}`,
          mb: 2,
        }}
      >
        <Box
          sx={{
            width: 56,
            height: 56,
            borderRadius: '50%',
            bgcolor: isDark ? 'rgba(0, 212, 255, 0.1)' : 'rgba(0, 150, 255, 0.1)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            mx: 'auto',
            mb: 2,
          }}
        >
          <CodeIcon sx={{ fontSize: 32, color: colors.accent }} />
        </Box>

        <Typography variant="h6" fontWeight={700} sx={{ mb: 1.5 }}>
          {t("tunnels.selfDeploy.title", "自部署服务")}
        </Typography>

        <Typography
          variant="body2"
          color="text.secondary"
          sx={{ mb: 2, maxWidth: 400, mx: 'auto', lineHeight: 1.6 }}
        >
          {t("tunnels.selfDeploy.description", "我们正在开发自部署功能，让您可以在自己的服务器上部署节点。此功能很快推出。")}
        </Typography>

        <Box
          sx={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 1,
            px: 2.5,
            py: 1,
            borderRadius: 1.5,
            bgcolor: isDark ? 'rgba(255, 193, 7, 0.1)' : 'rgba(255, 193, 7, 0.15)',
            border: `1px solid ${isDark ? 'rgba(255, 193, 7, 0.3)' : 'rgba(255, 193, 7, 0.4)'}`,
            mb: 1.5,
          }}
        >
          <Typography
            variant="body2"
            sx={{ color: isDark ? '#FFD54F' : '#F57C00', fontWeight: 600 }}
          >
            {t("tunnels.selfDeploy.comingSoon", "正在推进中，敬请期待")}
          </Typography>
        </Box>

        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ display: 'block', fontStyle: 'italic' }}
        >
          {t("tunnels.selfDeploy.openSourceNote", "推出后您可以自由部署和定制")}
        </Typography>
      </Paper>

      {/* Login CTA Card - Show for guests */}
      {!isAuthenticated && (
        <Paper
          elevation={0}
          sx={{
            p: 2.5,
            borderRadius: 2,
            textAlign: 'center',
            bgcolor: isDark ? 'rgba(76, 175, 80, 0.05)' : 'rgba(76, 175, 80, 0.04)',
            border: `1px solid ${isDark ? 'rgba(76, 175, 80, 0.2)' : 'rgba(76, 175, 80, 0.15)'}`,
          }}
        >
          <Box
            sx={{
              width: 48,
              height: 48,
              borderRadius: '50%',
              bgcolor: isDark ? 'rgba(76, 175, 80, 0.1)' : 'rgba(76, 175, 80, 0.1)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              mx: 'auto',
              mb: 1.5,
            }}
          >
            <CloudIcon sx={{ fontSize: 28, color: 'success.main' }} />
          </Box>

          <Typography variant="subtitle1" fontWeight={600} sx={{ mb: 1 }}>
            {t("tunnels.cloudNodes.title", "云端节点")}
          </Typography>

          <Typography
            variant="body2"
            color="text.secondary"
            sx={{ mb: 2, maxWidth: 320, mx: 'auto', fontSize: '0.875rem' }}
          >
            {t("tunnels.cloudNodes.description", "登录即可获取云端节点，无需自己部署，开箱即用。")}
          </Typography>

          <Button
            variant="contained"
            color="primary"
            size="medium"
            startIcon={<LoginIcon />}
            onClick={handleLogin}
            sx={{
              px: 4,
              py: 1,
              fontWeight: 600,
              borderRadius: 1.5,
            }}
          >
            {t("common:common.login", "登录")}
          </Button>
        </Paper>
      )}

      {/* Authenticated user info */}
      {isAuthenticated && (
        <Paper
          elevation={0}
          sx={{
            p: 2,
            borderRadius: 2,
            textAlign: 'center',
            bgcolor: isDark ? 'rgba(76, 175, 80, 0.05)' : 'rgba(76, 175, 80, 0.04)',
            border: `1px solid ${isDark ? 'rgba(76, 175, 80, 0.2)' : 'rgba(76, 175, 80, 0.15)'}`,
          }}
        >
          <Stack direction="row" spacing={1} alignItems="center" justifyContent="center">
            <CloudIcon sx={{ fontSize: 18, color: 'success.main' }} />
            <Typography variant="body2" color="text.secondary" sx={{ fontSize: '0.875rem' }}>
              {t("tunnels.cloudNodes.usingCloud", "您正在使用云端节点，返回主页选择节点开始使用")}
            </Typography>
          </Stack>
        </Paper>
      )}
    </Box>
  );
}

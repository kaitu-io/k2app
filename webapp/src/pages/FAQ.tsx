import { useState } from "react";
import { useNavigate, useLocation } from "react-router-dom";
import {
  Box,
  Typography,
  Card,
  CardContent,
  Alert,
  Button,
  Stack,
  CircularProgress,
} from "@mui/material";
import {
  Build as FixIcon,
  SupportAgent as SupportIcon,
  ChevronRight as ChevronRightIcon,
  Security as SecurityIcon,
  Forum as ForumIcon,
} from "@mui/icons-material";
import { useTranslation } from "react-i18next";
import SpeedTest from "../components/SpeedTest";
import BackButton from "../components/BackButton";
import { useAppLinks } from "../hooks/useAppLinks";

export default function FAQ() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const { links } = useAppLinks();

  // Get the previous page from navigation state, fallback to /account
  const backTo = (location.state as { from?: string })?.from || '/account';

  // 网络修复状态
  const [isFixingNetwork, setIsFixingNetwork] = useState(false);
  const [fixNetworkResult, setFixNetworkResult] = useState<{ success: boolean; message: string } | null>(null);

  // 网络修复处理
  const handleFixNetwork = async () => {
    setIsFixingNetwork(true);
    setFixNetworkResult(null);

    try {
      const response = await window._k2.run('fix_network');
      if (response.code === 0) {
        setFixNetworkResult({ success: true, message: t('dashboard:troubleshooting.fixNetwork.success') });
      } else if (response.code === 400) {
        setFixNetworkResult({ success: false, message: t('dashboard:troubleshooting.fixNetwork.unsupported') });
      } else {
        setFixNetworkResult({ success: false, message: t('dashboard:troubleshooting.fixNetwork.error') });
      }
    } catch (error) {
      console.error('Failed to fix network:', error);
      setFixNetworkResult({ success: false, message: t('dashboard:troubleshooting.fixNetwork.error') });
    } finally {
      setIsFixingNetwork(false);

      // 自动清除结果提示
      setTimeout(() => {
        setFixNetworkResult(null);
      }, 5000);
    }
  };

  return (
    <Box sx={{
      width: "100%",
      height: "100%",
      position: "relative"
    }}>
      <BackButton to={backTo} />

      <Box sx={{
        width: "100%",
        height: "100%",
        display: "flex",
        justifyContent: "center",
        alignItems: "flex-start",
        pt: 9
      }}>
        {/* 工具面板 */}
        <Box sx={{
          width: 500,
          display: "flex",
          flexDirection: "column",
          gap: 1.5,
          overflow: "auto",
          height: "100%",
          pr: 0.5,
          '&::-webkit-scrollbar': {
            width: '8px',
          },
          '&::-webkit-scrollbar-track': {
            background: 'transparent',
          },
          '&::-webkit-scrollbar-thumb': {
            background: (theme) => theme.palette.mode === 'dark' ? 'rgba(255,255,255,0.2)' : 'rgba(0,0,0,0.2)',
            borderRadius: '4px',
          },
          '&::-webkit-scrollbar-thumb:hover': {
            background: (theme) => theme.palette.mode === 'dark' ? 'rgba(255,255,255,0.3)' : 'rgba(0,0,0,0.3)',
          },
        }}>
        {/* 网络修复工具 */}
        <Card>
          <CardContent>
            <Stack spacing={2}>
              <Box display="flex" alignItems="center" gap={1}>
                <FixIcon color="action" />
                <Typography variant="h6">{t('dashboard:troubleshooting.fixNetwork.title')}</Typography>
              </Box>

              <Typography variant="body2" color="text.secondary">
                {t('dashboard:troubleshooting.fixNetwork.description')}
              </Typography>

              <Button
                variant="contained"
                color="secondary"
                onClick={handleFixNetwork}
                disabled={isFixingNetwork}
                startIcon={isFixingNetwork ? <CircularProgress size={20} color="inherit" /> : <FixIcon />}
                fullWidth
              >
                {isFixingNetwork ? t('dashboard:troubleshooting.fixNetwork.fixing') : t('dashboard:troubleshooting.fixNetwork.button')}
              </Button>

              {fixNetworkResult && (
                <Alert severity={fixNetworkResult.success ? "success" : "error"}>
                  {fixNetworkResult.message}
                </Alert>
              )}
            </Stack>
          </CardContent>
        </Card>

        {/* 安全软件白名单设置 */}
        <Card
          sx={{
            cursor: 'pointer',
            transition: 'all 0.2s',
            '&:hover': {
              bgcolor: 'action.hover',
            }
          }}
          onClick={() => window._platform!.openExternal?.(links.securitySoftwareHelpUrl)}
        >
          <CardContent>
            <Stack direction="row" alignItems="center" justifyContent="space-between">
              <Box display="flex" alignItems="center" gap={1.5}>
                <SecurityIcon color="warning" />
                <Box>
                  <Typography variant="subtitle1">{t('dashboard:troubleshooting.securitySoftware.title')}</Typography>
                  <Typography variant="body2" color="text.secondary">
                    {t('dashboard:troubleshooting.securitySoftware.description')}
                  </Typography>
                </Box>
              </Box>
              <ChevronRightIcon color="action" />
            </Stack>
          </CardContent>
        </Card>

        {/* 网速测试 */}
        <SpeedTest />

        {/* 社区反馈入口 */}
        <Card
          sx={{
            cursor: 'pointer',
            transition: 'all 0.2s',
            '&:hover': {
              bgcolor: 'action.hover',
            }
          }}
          onClick={() => navigate('/issues')}
        >
          <CardContent>
            <Stack direction="row" alignItems="center" justifyContent="space-between">
              <Box display="flex" alignItems="center" gap={1.5}>
                <ForumIcon color="info" />
                <Box>
                  <Typography variant="subtitle1">{t('ticket:issues.entryTitle')}</Typography>
                  <Typography variant="body2" color="text.secondary">
                    {t('ticket:issues.entryDescription')}
                  </Typography>
                </Box>
              </Box>
              <ChevronRightIcon color="action" />
            </Stack>
          </CardContent>
        </Card>

        {/* 提交工单入口 */}
        <Card
          sx={{
            cursor: 'pointer',
            transition: 'all 0.2s',
            '&:hover': {
              bgcolor: 'action.hover',
            }
          }}
          onClick={() => navigate('/submit-ticket')}
        >
          <CardContent>
            <Stack direction="row" alignItems="center" justifyContent="space-between">
              <Box display="flex" alignItems="center" gap={1.5}>
                <SupportIcon color="primary" />
                <Box>
                  <Typography variant="subtitle1">{t('ticket:ticket.entryTitle')}</Typography>
                  <Typography variant="body2" color="text.secondary">
                    {t('ticket:ticket.entryDescription')}
                  </Typography>
                </Box>
              </Box>
              <ChevronRightIcon color="action" />
            </Stack>
          </CardContent>
        </Card>
        </Box>
      </Box>
    </Box>
  );
}

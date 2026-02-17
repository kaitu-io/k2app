import { useNavigate, useLocation } from "react-router-dom";
import {
  Box,
  Typography,
  Card,
  CardContent,
  Stack,
} from "@mui/material";
import {
  SupportAgent as SupportIcon,
  ChevronRight as ChevronRightIcon,
  Security as SecurityIcon,
  Forum as ForumIcon,
} from "@mui/icons-material";
import { useTranslation } from "react-i18next";
import BackButton from "../components/BackButton";
import { useAppLinks } from "../hooks/useAppLinks";

export default function FAQ() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const { links } = useAppLinks();

  // Get the previous page from navigation state, fallback to /account
  const backTo = (location.state as { from?: string })?.from || '/account';

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

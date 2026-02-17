import { useNavigate, useLocation } from "react-router-dom";
import {
  Box,
  Typography,
  Card,
  CardContent,
  Button,
  Stack,
} from "@mui/material";
import {
  ErrorOutline as ErrorIcon,
  Update as UpdateIcon,
  Security as SecurityIcon,
  Download as DownloadIcon,
  SupportAgent as SupportIcon,
  ChevronRight as ChevronRightIcon,
} from "@mui/icons-material";
import { useTranslation } from "react-i18next";
import BackButton from "../components/BackButton";

export default function ServiceError() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();

  // Get the previous page from navigation state, fallback to /
  const backTo = (location.state as { from?: string })?.from || '/';

  const handleDownload = () => {
    window._platform?.openExternal?.('https://kaitu.io/install');
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
        pt: 9,
        px: 2,
      }}>
        <Box sx={{
          maxWidth: 500,
          width: "100%",
          display: "flex",
          flexDirection: "column",
          gap: 2,
          overflow: "auto",
          height: "100%",
          pb: 4,
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
        }}>
          {/* Header */}
          <Box sx={{ textAlign: "center", mb: 2 }}>
            <ErrorIcon sx={{ fontSize: 64, color: "error.main", mb: 2 }} />
            <Typography variant="h5" fontWeight={600} gutterBottom>
              {t('dashboard:dashboard.serviceError.title')}
            </Typography>
            <Typography variant="body2" color="text.secondary">
              {t('dashboard:dashboard.serviceError.subtitle')}
            </Typography>
          </Box>

          {/* Possible Reasons */}
          <Card variant="outlined">
            <CardContent>
              <Stack spacing={2.5}>
                {/* Reason 1: Auto-update failure */}
                <Box sx={{ display: "flex", gap: 2 }}>
                  <UpdateIcon sx={{ color: "warning.main", mt: 0.25 }} />
                  <Box>
                    <Typography variant="subtitle2" fontWeight={600}>
                      {t('dashboard:dashboard.serviceError.reason1Title')}
                    </Typography>
                    <Typography variant="body2" color="text.secondary">
                      {t('dashboard:dashboard.serviceError.reason1Desc')}
                    </Typography>
                  </Box>
                </Box>

                {/* Reason 2: Security software */}
                <Box sx={{ display: "flex", gap: 2 }}>
                  <SecurityIcon sx={{ color: "warning.main", mt: 0.25 }} />
                  <Box>
                    <Typography variant="subtitle2" fontWeight={600}>
                      {t('dashboard:dashboard.serviceError.reason2Title')}
                    </Typography>
                    <Typography variant="body2" color="text.secondary">
                      {t('dashboard:dashboard.serviceError.reason2Desc')}
                    </Typography>
                  </Box>
                </Box>
              </Stack>
            </CardContent>
          </Card>

          {/* Solution */}
          <Card sx={{ bgcolor: "primary.main", color: "primary.contrastText" }}>
            <CardContent>
              <Stack spacing={2}>
                <Box sx={{ display: "flex", gap: 2, alignItems: "flex-start" }}>
                  <DownloadIcon sx={{ mt: 0.25 }} />
                  <Box>
                    <Typography variant="subtitle1" fontWeight={600}>
                      {t('dashboard:dashboard.serviceError.solutionTitle')}
                    </Typography>
                    <Typography variant="body2" sx={{ opacity: 0.9 }}>
                      {t('dashboard:dashboard.serviceError.solutionDesc')}
                    </Typography>
                  </Box>
                </Box>

                <Button
                  variant="contained"
                  color="inherit"
                  onClick={handleDownload}
                  startIcon={<DownloadIcon />}
                  fullWidth
                  sx={{
                    bgcolor: "white",
                    color: "primary.main",
                    fontWeight: 600,
                    '&:hover': {
                      bgcolor: "grey.100",
                    }
                  }}
                >
                  {t('dashboard:dashboard.serviceError.downloadButton')}
                </Button>
              </Stack>
            </CardContent>
          </Card>

          {/* Persistent issue - Submit ticket */}
          <Card
            sx={{
              cursor: 'pointer',
              transition: 'all 0.2s',
              '&:hover': {
                bgcolor: 'action.hover',
              }
            }}
            onClick={() => navigate('/faq', { state: { from: location.pathname } })}
          >
            <CardContent>
              <Stack direction="row" alignItems="center" justifyContent="space-between">
                <Box display="flex" alignItems="center" gap={1.5}>
                  <SupportIcon color="primary" />
                  <Box>
                    <Typography variant="subtitle1">
                      {t('dashboard:dashboard.serviceError.persistentTitle')}
                    </Typography>
                    <Typography variant="body2" color="text.secondary">
                      {t('dashboard:dashboard.serviceError.persistentDesc')}
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

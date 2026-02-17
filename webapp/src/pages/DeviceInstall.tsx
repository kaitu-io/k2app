import { useState, useEffect } from "react";
import {
  Box,
  Typography,
  Button,
  Stack,
  alpha,
} from "@mui/material";
import {
  ContentCopy as CopyIcon,
  PhoneIphone as IOSIcon,
  PhoneAndroid as AndroidIcon,
  Computer as WindowsIcon,
  Laptop as MacIcon,
  Download as DownloadIcon,
} from "@mui/icons-material";
import { useTranslation } from "react-i18next";
import QRCode from "qrcode";
import { useAlert } from "../stores";

import type { AppConfig } from "../services/api-types";
import BackButton from "../components/BackButton";
import { k2api } from '../services/k2api';

// 默认的下载链接
const DEFAULT_INSTALL_URL = "https://kaitu.io/install";

export default function DeviceInstall() {
  const { t } = useTranslation();
  const [qrCode, setQrCode] = useState<string>("");
  const [installURL, setInstallURL] = useState<string>(DEFAULT_INSTALL_URL);
  const { showAlert } = useAlert();

  // 获取应用配置并生成二维码
  useEffect(() => {
    const loadAppConfig = async () => {
      try {
        // 获取应用配置
        const response = await k2api().exec<AppConfig>('api_request', {
          method: 'GET',
          path: '/api/app/config',
        });
        if (response.code === 0 && response.data?.appLinks) {
          const { baseURL, installPath } = response.data.appLinks;
          if (baseURL && installPath) {
            const cleanBaseUrl = baseURL.replace(/\/$/, '');
            const cleanPath = installPath.replace(/^\//, '');
            setInstallURL(`${cleanBaseUrl}/${cleanPath}`);
          }
        }
      } catch (error) {
        console.error('Failed to load app config:', error);
        // 使用默认链接
      }
    };

    const initialize = async () => {
      await loadAppConfig();
      // 在状态更新后生成二维码
    };

    initialize();
  }, [t]);

  // 当installURL变化时重新生成二维码
  useEffect(() => {
    if (installURL) {
      const generateQRCode = async () => {
        try {
          const qrCodeDataURL = await QRCode.toDataURL(installURL, {
            width: 200,
            margin: 2,
            color: {
              dark: "#1976d2",
              light: "#FFFFFF"
            }
          });
          setQrCode(qrCodeDataURL);
        } catch (error) {
          console.error(t('common:common.error'), error);
        }
      };

      generateQRCode();
    }
  }, [installURL, t]);

  const handleCopyLink = async () => {
    try {
      await window._platform!.writeClipboard?.(installURL);
      showAlert(t('common:messages.copySuccess'), "success");
    } catch (error) {
      console.error(t('common:messages.copyFailed'));
      showAlert(t('common:messages.copyFailed'), "error");
    }
  };

  return (
    <Box
      sx={{
        width: "100%",
        py: 0.5,
        backgroundColor: "transparent",
        position: "relative"
      }}
    >
      <BackButton to="/account" />
      {/* 主内容区域 */}
      <Box
        sx={{
          overflow: "auto",
          px: 0.5,
          py: 1,
          pt: 7,
        }}
      >
        <Box
          sx={{
            width: "100%",
          }}
        >
          {/* 客户端下载卡片 */}
          <Box
            sx={{
              borderRadius: 3,
              border: "2px solid",
              borderColor: (theme) => theme.palette.mode === 'dark'
                ? 'rgba(25, 118, 210, 0.3)'
                : 'rgba(25, 118, 210, 0.2)',
              backgroundColor: (theme) => theme.palette.background.paper,
              boxShadow: '0 4px 12px rgba(0, 0, 0, 0.05)',
              transition: 'all 0.3s ease',
              '&:hover': {
                boxShadow: '0 6px 20px rgba(25, 118, 210, 0.15)',
                transform: 'translateY(-2px)',
              },
            }}
          >
            <Box sx={{ p: 1.5 }}>
              <Stack spacing={2}>
                {/* 标题和平台图标 */}
                <Box sx={{
                  display: "flex",
                  alignItems: "center",
                  gap: 3,
                  flexDirection: "column",
                }}>
                  <Box sx={{
                    textAlign: "center",
                    width: "100%",
                  }}>
                    <Box sx={{
                      display: "flex",
                      alignItems: "center",
                      gap: 1.5,
                      mb: 1.5,
                      justifyContent: "center",
                    }}>
                      <Box sx={{
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        width: 48,
                        height: 48,
                        borderRadius: '50%',
                        bgcolor: (theme) => theme.palette.mode === 'dark'
                          ? 'rgba(25, 118, 210, 0.2)'
                          : 'rgba(25, 118, 210, 0.1)',
                      }}>
                        <DownloadIcon
                          sx={{
                            fontSize: 28,
                            color: "primary.main"
                          }}
                        />
                      </Box>
                      <Typography variant="h5" fontWeight={700} color="primary.main">
                        {t('purchase:deviceInstall.title')}
                      </Typography>
                    </Box>
                    <Typography variant="body1" color="text.secondary" sx={{ fontSize: '0.95rem' }}>
                      {t('purchase:deviceInstall.platformSupport')}
                    </Typography>
                  </Box>

                  {/* 平台图标 */}
                  <Box
                    sx={{
                      display: "flex",
                      gap: 1.5,
                      flexWrap: "wrap",
                      justifyContent: "center",
                    }}
                  >
                    {[
                      { icon: <IOSIcon />, color: "#007AFF", name: "iOS" },
                      { icon: <AndroidIcon />, color: "#34C759", name: "Android" },
                      { icon: <WindowsIcon />, color: "#0078D4", name: "Windows" },
                      { icon: <MacIcon />, color: "#666666", name: "macOS" },
                    ].map((platform) => (
                      <Box
                        key={platform.name}
                        sx={{
                          width: 48,
                          height: 48,
                          borderRadius: 2,
                          bgcolor: alpha(platform.color, 0.1),
                          display: "flex",
                          alignItems: "center",
                          justifyContent: "center",
                          color: platform.color,
                          border: `2px solid ${alpha(platform.color, 0.2)}`,
                          transition: 'all 0.2s',
                          '&:hover': {
                            transform: 'scale(1.1)',
                            boxShadow: `0 4px 12px ${alpha(platform.color, 0.3)}`,
                          },
                          "& svg": {
                            fontSize: 24,
                          },
                        }}
                      >
                        {platform.icon}
                      </Box>
                    ))}
                  </Box>
                </Box>

                {/* 二维码和下载链接 - 移动端布局 */}
                <Box sx={{
                  display: "flex",
                  alignItems: "stretch",
                  gap: 2,
                  flexDirection: "column",
                  p: 1.5,
                  borderRadius: 2,
                  bgcolor: (theme) => theme.palette.mode === 'dark'
                    ? 'rgba(255, 255, 255, 0.02)'
                    : 'rgba(0, 0, 0, 0.02)',
                }}>
                  {/* 二维码 */}
                  {qrCode && (
                    <Box sx={{
                      textAlign: "center",
                      display: "flex",
                      flexDirection: "column",
                      alignItems: "center",
                      width: "100%",
                    }}>
                      <Box sx={{
                        p: 1,
                        borderRadius: 2,
                        bgcolor: 'white',
                        boxShadow: '0 2px 8px rgba(0, 0, 0, 0.1)',
                      }}>
                        <img
                          src={qrCode}
                          alt={t('common:common.download')}
                          style={{
                            width: 140,
                            height: 140,
                            borderRadius: 4,
                          }}
                        />
                      </Box>
                      <Typography
                        variant="body2"
                        color="text.secondary"
                        display="block"
                        sx={{ mt: 1.5, fontWeight: 500 }}
                      >
                        {t('purchase:deviceInstall.scanToDownload')}
                      </Typography>
                    </Box>
                  )}

                  {/* 下载链接 */}
                  <Box sx={{
                    width: "100%",
                  }}>
                    <Typography
                      variant="body2"
                      color="text.secondary"
                      sx={(theme) => ({
                        fontFamily: "monospace",
                        wordBreak: "break-all",
                        bgcolor: theme.palette.mode === 'dark'
                          ? alpha("#fff", 0.08)
                          : alpha("#000", 0.04),
                        p: 1.5,
                        borderRadius: 2,
                        mb: 1.5,
                        textAlign: "center",
                        border: '1px dashed',
                        borderColor: theme.palette.mode === 'dark'
                          ? 'rgba(255, 255, 255, 0.1)'
                          : 'rgba(0, 0, 0, 0.1)',
                        fontSize: '0.85rem',
                      })}
                    >
                      {installURL}
                    </Typography>

                    <Button
                      variant="contained"
                      fullWidth
                      startIcon={<CopyIcon />}
                      onClick={handleCopyLink}
                      sx={{
                        background: 'linear-gradient(135deg, #1976d2 0%, #42a5f5 100%)',
                        "&:hover": {
                          background: 'linear-gradient(135deg, #1565c0 0%, #1976d2 100%)',
                          boxShadow: '0 4px 12px rgba(25, 118, 210, 0.4)',
                          transform: 'translateY(-2px)',
                        },
                        textTransform: "none",
                        fontWeight: 700,
                        borderRadius: 2,
                        py: 1.25,
                        fontSize: '0.95rem',
                        boxShadow: '0 2px 8px rgba(25, 118, 210, 0.3)',
                        transition: 'all 0.3s ease',
                      }}
                    >
                      {t('purchase:deviceInstall.copyDownloadLink')}
                    </Button>
                  </Box>
                </Box>
              </Stack>
            </Box>
          </Box>

          {/* 底部说明 */}
          <Box sx={{
            mt: 2,
            p: 1.5,
            borderRadius: 2,
            bgcolor: (theme) => theme.palette.mode === 'dark'
              ? 'rgba(66, 165, 245, 0.05)'
              : 'rgba(25, 118, 210, 0.03)',
            border: '1px solid',
            borderColor: (theme) => theme.palette.mode === 'dark'
              ? 'rgba(66, 165, 245, 0.1)'
              : 'rgba(25, 118, 210, 0.1)',
          }}>
            <Stack spacing={0.75} alignItems="center">
              <Typography variant="body2" color="text.secondary" textAlign="center" sx={{ fontWeight: 500 }}>
                {t('purchase:deviceInstall.multiDeviceSync')}
              </Typography>
              <Typography variant="body2" color="primary.main" textAlign="center" sx={{ fontWeight: 600 }}>
                {t('purchase:deviceInstall.contactSupport')}
              </Typography>
            </Stack>
          </Box>
        </Box>
      </Box>
    </Box>
  );
}
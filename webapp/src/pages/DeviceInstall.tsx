import { useState, useEffect } from "react";
import {
  Box,
  Typography,
  Button,
  Stack,
  Chip,
  alpha,
  SvgIcon,
} from "@mui/material";
import {
  ContentCopy as CopyIcon,
  Share as ShareIcon,
  Download as DownloadIcon,
  Usb as UsbIcon,
  ChevronRight as ChevronRightIcon,
} from "@mui/icons-material";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import QRCode from "qrcode";
import { useAlert } from "../stores";

import type { AppConfig } from "../services/api-types";
import BackButton from "../components/BackButton";
import { cloudApi } from '../services/cloud-api';

// 默认的下载链接
const DEFAULT_INSTALL_URL = "https://kaitu.io/install";

// Brand SVG icons
function AppleIcon(props: React.ComponentProps<typeof SvgIcon>) {
  return (
    <SvgIcon {...props} viewBox="0 0 24 24">
      <path d="M18.71 19.5c-.83 1.24-1.71 2.45-3.05 2.47-1.34.03-1.77-.79-3.29-.79-1.53 0-2 .77-3.27.82-1.31.05-2.3-1.32-3.14-2.53C4.25 17 2.94 12.45 4.7 9.39c.87-1.52 2.43-2.48 4.12-2.51 1.28-.02 2.5.87 3.29.87.78 0 2.26-1.07 3.8-.91.65.03 2.47.26 3.64 1.98-.09.06-2.17 1.28-2.15 3.81.03 3.02 2.65 4.03 2.68 4.04-.03.07-.42 1.44-1.38 2.83M13 3.5c.73-.83 1.94-1.46 2.94-1.5.13 1.17-.34 2.35-1.04 3.19-.69.85-1.83 1.51-2.95 1.42-.15-1.15.41-2.35 1.05-3.11z" />
    </SvgIcon>
  );
}

function AndroidIcon(props: React.ComponentProps<typeof SvgIcon>) {
  return (
    <SvgIcon {...props} viewBox="0 0 24 24">
      <path d="M17.6 9.48l1.84-3.18c.16-.31.04-.69-.26-.85-.29-.15-.65-.06-.83.22l-1.88 3.24C14.86 8.32 13.47 8 12 8s-2.86.32-4.47.91L5.65 5.67c-.18-.28-.54-.37-.83-.22-.3.16-.42.54-.26.85L6.4 9.48C3.3 11.25 1.28 14.44 1 18h22c-.28-3.56-2.3-6.75-5.4-8.52zM7 15.25a1.25 1.25 0 110-2.5 1.25 1.25 0 010 2.5zm10 0a1.25 1.25 0 110-2.5 1.25 1.25 0 010 2.5z" />
    </SvgIcon>
  );
}

function WindowsIcon(props: React.ComponentProps<typeof SvgIcon>) {
  return (
    <SvgIcon {...props} viewBox="0 0 24 24">
      <path d="M3 12V6.75l6-1.32v6.48L3 12zm17-9v8.75l-10 .08V5.21L20 3zM3 13l6 .09v6.81l-6-1.15V13zm17 .25V22l-10-1.91V13.1l10 .15z" />
    </SvgIcon>
  );
}

export default function DeviceInstall() {
  const { t } = useTranslation();
  const [qrCode, setQrCode] = useState<string>("");
  const [installURL, setInstallURL] = useState<string>(DEFAULT_INSTALL_URL);
  const { showAlert } = useAlert();
  const navigate = useNavigate();
  const isDesktop = window._platform?.os === 'macos' || window._platform?.os === 'windows' || window._platform?.os === 'linux';

  // 获取应用配置并生成二维码
  useEffect(() => {
    const loadAppConfig = async () => {
      try {
        const response = await cloudApi.get<AppConfig>('/api/app/config');
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
      }
    };

    loadAppConfig();
  }, []);

  // 桌面端才需要二维码
  useEffect(() => {
    if (!isDesktop || !installURL) return;

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
        console.error('QR code generation failed:', error);
      }
    };

    generateQRCode();
  }, [installURL, isDesktop]);

  const handleCopyLink = async () => {
    try {
      await window._platform?.writeClipboard?.(installURL);
      showAlert(t('common:messages.copySuccess'), "success");
    } catch (error) {
      console.error(t('common:messages.copyFailed'));
      showAlert(t('common:messages.copyFailed'), "error");
    }
  };

  const handleShare = async () => {
    try {
      const shareText = `${t('purchase:deviceInstall.shareText')} ${installURL}`;
      // Prefer platform share (native share sheet on mobile)
      if (window._platform?.share) {
        await window._platform.share({ title: 'Kaitu', text: shareText, url: installURL });
      } else if (navigator.share) {
        await navigator.share({ title: 'Kaitu', text: t('purchase:deviceInstall.shareText'), url: installURL });
      } else {
        // fallback: copy full share text with link
        await window._platform?.writeClipboard?.(shareText);
        showAlert(t('common:messages.copySuccess'), "success");
      }
    } catch (error) {
      // user cancelled share — ignore AbortError
      if (error instanceof Error && error.name !== 'AbortError') {
        console.error('Share failed:', error);
      }
    }
  };

  const platforms = [
    { icon: <AppleIcon />, color: "#999999", name: "iOS" },
    { icon: <AndroidIcon />, color: "#3DDC84", name: "Android" },
    { icon: <WindowsIcon />, color: "#0078D4", name: "Windows" },
    { icon: <AppleIcon />, color: "#999999", name: "macOS" },
  ];

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
          {/* 安卓 USB 安装入口 - 仅桌面端，置顶 */}
          {isDesktop && (
            <Box
              onClick={() => navigate('/android-install')}
              sx={{
                mb: 2,
                p: 2,
                borderRadius: 3,
                border: '2px solid',
                borderColor: (theme) => theme.palette.mode === 'dark'
                  ? 'rgba(52, 199, 89, 0.3)'
                  : 'rgba(52, 199, 89, 0.2)',
                bgcolor: (theme) => theme.palette.background.paper,
                cursor: 'pointer',
                display: 'flex',
                alignItems: 'center',
                gap: 2,
                transition: 'all 0.3s ease',
                '&:hover': {
                  borderColor: 'rgba(52, 199, 89, 0.5)',
                  boxShadow: '0 4px 16px rgba(52, 199, 89, 0.15)',
                  transform: 'translateY(-2px)',
                },
              }}
            >
              <Box sx={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                width: 48,
                height: 48,
                borderRadius: '50%',
                bgcolor: 'rgba(52, 199, 89, 0.15)',
                flexShrink: 0,
              }}>
                <UsbIcon sx={{ fontSize: 24, color: '#34C759' }} />
              </Box>
              <Box sx={{ flex: 1 }}>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                  <Typography variant="body1" fontWeight={600}>
                    {t('purchase:deviceInstall.androidInstallCard')}
                  </Typography>
                  <Chip
                    label={t('common:common.experimental')}
                    size="small"
                    sx={{
                      height: 20,
                      fontSize: '0.7rem',
                      fontWeight: 600,
                      bgcolor: 'rgba(255, 152, 0, 0.15)',
                      color: '#FF9800',
                      border: '1px solid rgba(255, 152, 0, 0.3)',
                    }}
                  />
                </Box>
                <Typography variant="body2" color="text.secondary">
                  {t('purchase:deviceInstall.androidInstallCardDesc')}
                </Typography>
              </Box>
              <ChevronRightIcon sx={{ color: 'text.secondary' }} />
            </Box>
          )}

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

                  {/* 平台图标 + 名称 */}
                  <Box
                    sx={{
                      display: "flex",
                      gap: 2,
                      flexWrap: "wrap",
                      justifyContent: "center",
                    }}
                  >
                    {platforms.map((platform) => (
                      <Box
                        key={platform.name}
                        sx={{
                          display: "flex",
                          flexDirection: "column",
                          alignItems: "center",
                          gap: 0.5,
                        }}
                      >
                        <Box
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
                        <Typography variant="caption" color="text.secondary" sx={{ fontSize: '0.7rem' }}>
                          {platform.name}
                        </Typography>
                      </Box>
                    ))}
                  </Box>
                </Box>

                {/* 二维码（桌面端）或 分享按钮（移动端） */}
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
                  {/* 桌面端：二维码 */}
                  {isDesktop && qrCode && (
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

                  {/* 操作按钮 */}
                  <Box sx={{ width: "100%" }}>
                    {/* 桌面端显示链接文本 */}
                    {isDesktop && (
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
                    )}

                    <Stack spacing={1.5}>
                      {/* 移动端：分享按钮（主按钮） */}
                      {!isDesktop && (
                        <Button
                          variant="contained"
                          fullWidth
                          startIcon={<ShareIcon />}
                          onClick={handleShare}
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
                          {t('purchase:deviceInstall.shareToFriends')}
                        </Button>
                      )}

                      {/* 复制链接按钮 */}
                      <Button
                        variant={isDesktop ? "contained" : "outlined"}
                        fullWidth
                        startIcon={<CopyIcon />}
                        onClick={handleCopyLink}
                        sx={isDesktop ? {
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
                        } : {
                          textTransform: "none",
                          fontWeight: 600,
                          borderRadius: 2,
                          py: 1,
                          fontSize: '0.9rem',
                          transition: 'all 0.3s ease',
                        }}
                      >
                        {t('purchase:deviceInstall.copyDownloadLink')}
                      </Button>
                    </Stack>
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

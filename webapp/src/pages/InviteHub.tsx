import { useState, useEffect, useRef } from "react";
import {
  Box,
  Typography,
  Card,
  Button,
  TextField,
  IconButton,
  Stack,
  Paper,
  alpha,
  Collapse,
  Divider,
  Avatar,
  CircularProgress,
  Tooltip,
} from "@mui/material";
import {
  ContentCopy as CopyIcon,
  Edit as EditIcon,
  Save as SaveIcon,
  Close as CloseIcon,
  ArrowForwardIos as ArrowIcon,
  Download as DownloadIcon,
  ShoppingCart as ShoppingCartIcon,
  QrCode2 as QrCodeIcon,
  Share as ShareIcon,
  Link as LinkIcon,
} from "@mui/icons-material";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import QRCode from "qrcode";

import type { MyInviteCode } from "../services/api-types";
import { useAlert, useAuth } from "../stores";
import { useUser } from "../hooks/useUser";
import { useShareLink } from "../hooks/useShareLink";
import { useInviteCodeActions } from "../hooks/useInviteCodeActions";
import { useAppConfig } from "../hooks/useAppConfig";
import RetailerStatsOverview from "../components/RetailerStatsOverview";
import InviteRule from "../components/InviteRule";
import { cloudApi } from '../services/cloud-api';
import { delayedFocus } from '../utils/ui';

export default function Invite() {
  const { t } = useTranslation();
  const [invite, setInvite] = useState<MyInviteCode | null>(null);
  const [editRemark, setEditRemark] = useState("");
  const [editing, setEditing] = useState(false);

  // Ref for delayed focus when editing
  const remarkInputRef = useRef<HTMLInputElement>(null);

  // Delayed focus when entering edit mode
  useEffect(() => {
    if (!editing) return;
    const cancel = delayedFocus(() => remarkInputRef.current, 100);
    return cancel;
  }, [editing]);
  const [loadingInviteCode, setLoadingInviteCode] = useState(true);
  const [inviteCodeError, setInviteCodeError] = useState<string | null>(null);
  const [qrCodeUrl, setQrCodeUrl] = useState<string>("");
  const [qrCodeError, setQrCodeError] = useState<string | null>(null);
  const [qrCodeLoading, setQrCodeLoading] = useState(false);
  const navigate = useNavigate();
  const { showAlert } = useAlert();
  const { user } = useUser();
  const { isAuthenticated } = useAuth();
  const { getShareLink, loading: shareLinkLoading } = useShareLink();
  const { shareInviteCode } = useInviteCodeActions();
  const { appConfig } = useAppConfig();

  const baseURL = appConfig?.appLinks?.baseURL || 'https://kaitu.io';
  const promotionLink = invite ? `${baseURL}/s/${invite.code}` : '';

  const isMobile = ['ios', 'android'].includes(window._platform!.os) || /Mobile|Android|iPhone|iPad/i.test(navigator.userAgent);

  // Load latest invite code on auth
  useEffect(() => {
    if (isAuthenticated) {
      loadLatestInviteCode();
    }
  }, [isAuthenticated]);

  const loadLatestInviteCode = async () => {
    try {
      setLoadingInviteCode(true);
      setInviteCodeError(null);
      const response = await cloudApi.get<MyInviteCode>('/api/invite/my-codes/latest');
      if (response.code === 0 && response.data) {
        setInvite(response.data);
        setInviteCodeError(null);
      } else {
        const userMessage = t('invite:invite.loadInviteCodeFailed');
        setInviteCodeError(userMessage);
        console.error('[InviteHub] Failed to load invite code:', response.message);
      }
    } catch (error) {
      const errorMsg = t('invite:invite.loadInviteCodeFailedShort');
      setInviteCodeError(errorMsg);
      console.error(t('invite:invite.loadInviteCodeFailedShort') + ": " + (error as Error).message);
    } finally {
      setLoadingInviteCode(false);
    }
  };

  // Generate QR code from short link
  const generateQRCode = async () => {
    if (!invite) return;

    setQrCodeLoading(true);
    setQrCodeError(null);
    setQrCodeUrl('');

    try {
      const shareLink = await getShareLink(invite.code);
      if (!shareLink) {
        const errorMsg = t('invite:invite.getShareLinkFailed', '获取分享链接失败');
        console.error('Failed to get share link for QR code');
        setQrCodeError(errorMsg);
        setQrCodeLoading(false);
        return;
      }

      const qrDataURL = await QRCode.toDataURL(shareLink, {
        width: 300,
        margin: 1,
        color: {
          dark: '#1976d2',
          light: '#ffffff',
        },
      });
      setQrCodeUrl(qrDataURL);
      setQrCodeError(null);
    } catch (error) {
      console.error('QR Code generation failed:', error);
      const errorMsg = t('invite:invite.qrCodeGenerationFailed', '二维码生成失败');
      setQrCodeError(errorMsg);
    } finally {
      setQrCodeLoading(false);
    }
  };

  // Regenerate QR code when invite changes
  useEffect(() => {
    if (invite) {
      generateQRCode();
    }
  }, [invite]);

  // Share / copy share content
  const handleCopyShareContent = () => {
    if (!invite) return;
    shareInviteCode(invite);
  };

  // Copy promotion link
  const handleCopyPromotionLink = async () => {
    try {
      await window._platform!.writeClipboard?.(promotionLink);
      showAlert(t('invite:invite.promotionLinkCopied'), "success");
    } catch (error) {
      console.error(t('invite:invite.copyFailed'));
      showAlert(t('invite:invite.copyFailedPermission'), "error");
    }
  };

  // Edit remark
  const handleEdit = () => {
    if (invite) {
      setEditRemark(invite.remark);
      setEditing(true);
    }
  };

  const handleSave = async () => {
    if (!invite) return;

    try {
      const response = await cloudApi.request<MyInviteCode>('PUT', `/api/invite/my-codes/${invite.code}/remark`, { remark: editRemark });
      if (response.code === 0) {
        setInvite({ ...invite, remark: editRemark });
        setEditing(false);
        showAlert(t('invite:invite.remarkUpdated'), "success");
      } else {
        console.error('[InviteHub] Update remark failed:', response.code, response.message);
        showAlert(t('invite:invite.updateRemarkFailed'), 'error');
      }
    } catch (error) {
      console.error('[InviteHub] Update remark failed:', error);
      showAlert(t('invite:invite.updateRemarkFailedRetry'), "error");
    }
  };

  // Render invite code card content (loading / error / empty states)
  const renderInviteCodeContent = () => {
    if (loadingInviteCode) {
      return (
        <Box sx={{ textAlign: 'center', py: 4 }}>
          <CircularProgress size={32} />
          <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
            {t('invite:invite.loadingInviteCode', '正在加载邀请码...')}
          </Typography>
        </Box>
      );
    }

    if (inviteCodeError) {
      return (
        <Box sx={{ textAlign: 'center', py: 4 }}>
          <Typography variant="body2" color="error" sx={{ mb: 2 }}>
            {inviteCodeError}
          </Typography>
          <Button
            variant="outlined"
            size="small"
            onClick={loadLatestInviteCode}
            sx={{
              borderRadius: 2,
              textTransform: "none",
            }}
          >
            {t('common:common.retry', '重试')}
          </Button>
        </Box>
      );
    }

    if (!invite) {
      return (
        <Box sx={{ textAlign: 'center', py: 4 }}>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {t('invite:invite.noInviteCode', '暂无邀请码')}
          </Typography>
          <Button
            variant="outlined"
            size="small"
            onClick={loadLatestInviteCode}
            sx={{
              borderRadius: 2,
              textTransform: "none",
            }}
          >
            {t('common:common.retry', '重试')}
          </Button>
        </Box>
      );
    }

    return null;
  };

  return (
    <Box
      sx={{
        width: "100%",
        minHeight: "100vh",
        backgroundColor: "transparent",
      }}
      data-tour="invite-page"
    >
      {/* Header Section */}
      <Box sx={{ mb: 2 }}>
        <Stack direction="row" alignItems="center" justifyContent="space-between">
          <Typography variant="h5" fontWeight={600}>
            {user?.isRetailer ? t('invite:invite.retailerTitle') : t('invite:invite.inviteFriends')}
          </Typography>
          <Button
            variant="text"
            size="small"
            endIcon={<ArrowIcon sx={{ fontSize: 14 }} />}
            onClick={() => navigate("/invite-codes")}
            sx={{
              textTransform: "none",
              fontWeight: 600,
              fontSize: "0.875rem",
              color: "primary.main",
            }}
          >
            {t('invite:invite.viewAll')}
          </Button>
        </Stack>
      </Box>

      {/* Intro Section */}
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2, lineHeight: 1.6 }}>
        {t('invite:invite.introBody')}
      </Typography>

      {/* Main Content */}
      <Box>
        {/* Invite Code Card */}
        <Card
          elevation={0}
          sx={{
            mb: 2,
            borderRadius: 3,
            border: "1px solid",
            borderColor: "divider",
            overflow: "hidden",
          }}
        >
          <Box sx={{ p: 2.5 }}>
            {renderInviteCodeContent() || (
              <>
                {/* Stats Row */}
                <Stack direction="row" spacing={2} sx={{ mb: 2.5 }}>
                  <Paper
                    elevation={0}
                    sx={(theme) => ({
                      flex: 1,
                      p: 1.5,
                      borderRadius: 2,
                      bgcolor: theme.palette.mode === 'dark'
                        ? alpha(theme.palette.success.main, 0.08)
                        : alpha(theme.palette.success.light, 0.1),
                      border: "1px solid",
                      borderColor: theme.palette.mode === 'dark'
                        ? alpha(theme.palette.success.main, 0.2)
                        : alpha(theme.palette.success.light, 0.3),
                    })}
                  >
                    <Stack direction="row" alignItems="center" spacing={1.5}>
                      <Avatar
                        sx={{
                          width: 36,
                          height: 36,
                          bgcolor: "success.main",
                        }}
                      >
                        <DownloadIcon sx={{ fontSize: 20 }} />
                      </Avatar>
                      <Box sx={{ flex: 1, minWidth: 0 }}>
                        <Typography variant="caption" color="text.secondary" sx={{ display: "block", mb: 0.25 }}>
                          {t('invite:invite.registered')}
                        </Typography>
                        <Typography variant="h6" fontWeight={700} color="success.main" sx={{ lineHeight: 1 }}>
                          {invite?.registerCount || 0} {t('invite:invite.people')}
                        </Typography>
                      </Box>
                    </Stack>
                  </Paper>

                  <Paper
                    elevation={0}
                    sx={(theme) => ({
                      flex: 1,
                      p: 1.5,
                      borderRadius: 2,
                      bgcolor: theme.palette.mode === 'dark'
                        ? alpha(theme.palette.warning.main, 0.08)
                        : alpha(theme.palette.warning.light, 0.1),
                      border: "1px solid",
                      borderColor: theme.palette.mode === 'dark'
                        ? alpha(theme.palette.warning.main, 0.2)
                        : alpha(theme.palette.warning.light, 0.3),
                    })}
                  >
                    <Stack direction="row" alignItems="center" spacing={1.5}>
                      <Avatar
                        sx={{
                          width: 36,
                          height: 36,
                          bgcolor: "warning.main",
                        }}
                      >
                        <ShoppingCartIcon sx={{ fontSize: 20 }} />
                      </Avatar>
                      <Box sx={{ flex: 1, minWidth: 0 }}>
                        <Typography variant="caption" color="text.secondary" sx={{ display: "block", mb: 0.25 }}>
                          {t('invite:invite.purchased')}
                        </Typography>
                        <Typography variant="h6" fontWeight={700} color="warning.main" sx={{ lineHeight: 1 }}>
                          {invite?.purchaseCount || 0} {t('invite:invite.people')}
                        </Typography>
                      </Box>
                    </Stack>
                  </Paper>
                </Stack>

                <Divider sx={{ my: 2 }} />

                {/* Invite Code Display */}
                <Box sx={{ mb: 0 }}>
                  <Tooltip title={t('invite:invite.clickToCopy', '点击复制邀请码')} arrow>
                    <Paper
                      elevation={0}
                      onClick={async () => {
                        if (!invite) return;
                        try {
                          await window._platform!.writeClipboard?.(invite.code.toUpperCase());
                          showAlert(t('invite:invite.inviteCodeCopied', '邀请码已复制'), "success");
                        } catch (error) {
                          showAlert(t('invite:invite.copyFailed', '复制失败'), "error");
                        }
                      }}
                      sx={(theme) => ({
                        p: 1.5,
                        borderRadius: 2,
                        bgcolor: theme.palette.mode === 'dark'
                          ? alpha(theme.palette.primary.main, 0.08)
                          : alpha(theme.palette.primary.light, 0.1),
                        border: "2px solid",
                        borderColor: "primary.main",
                        textAlign: "center",
                        cursor: "pointer",
                        transition: "all 0.2s",
                        "&:hover": {
                          bgcolor: theme.palette.mode === 'dark'
                            ? alpha(theme.palette.primary.main, 0.15)
                            : alpha(theme.palette.primary.light, 0.2),
                          transform: "scale(1.02)",
                        },
                        "&:active": {
                          transform: "scale(0.98)",
                        },
                      })}
                    >
                      <Typography
                        variant="h5"
                        sx={{
                          fontFamily: "monospace",
                          fontWeight: 800,
                          letterSpacing: 3,
                          color: "primary.main",
                        }}
                      >
                        {invite?.code.toUpperCase()}
                      </Typography>
                    </Paper>
                  </Tooltip>

                  {/* Remark Section */}
                  <Box sx={{ mt: 1.5 }}>
                    <Collapse in={!editing}>
                      <Stack direction="row" alignItems="center" spacing={1}>
                        <Typography variant="caption" color="text.secondary">
                          {t('invite:invite.remark')}：{invite?.remark || t('invite:invite.noRemark')}
                        </Typography>
                        <IconButton size="small" onClick={handleEdit} sx={{ p: 0.5 }}>
                          <EditIcon sx={{ fontSize: 14 }} />
                        </IconButton>
                      </Stack>
                    </Collapse>
                    <Collapse in={editing}>
                      <Stack direction="row" alignItems="center" spacing={1}>
                        <TextField
                          size="small"
                          fullWidth
                          value={editRemark}
                          onChange={(e) => setEditRemark(e.target.value)}
                          onBlur={(e) => setEditRemark(e.target.value.trim())}
                          onKeyDown={(e) => {
                            if (e.key === 'Enter') {
                              handleSave();
                            } else if (e.key === 'Escape') {
                              setEditing(false);
                            }
                          }}
                          placeholder={t('invite:invite.addRemark')}
                          variant="outlined"
                          inputRef={remarkInputRef}
                          inputProps={{
                            autoCapitalize: "sentences",
                            autoCorrect: "on",
                            spellCheck: true,
                          }}
                          sx={{
                            '& .MuiInputBase-root': { fontSize: '0.875rem' }
                          }}
                        />
                        <IconButton size="small" onClick={handleSave} color="primary">
                          <SaveIcon sx={{ fontSize: 18 }} />
                        </IconButton>
                        <IconButton size="small" onClick={() => setEditing(false)}>
                          <CloseIcon sx={{ fontSize: 18 }} />
                        </IconButton>
                      </Stack>
                    </Collapse>
                  </Box>
                </Box>
              </>
            )}
          </Box>
        </Card>

        {/* Share with Friends Card */}
        {invite && (
          <Card
            elevation={0}
            sx={{
              mb: 2,
              borderRadius: 3,
              border: "1px solid",
              borderColor: "divider",
              overflow: "hidden",
            }}
          >
            <Box sx={{ p: 2.5 }}>
              <Typography variant="subtitle1" fontWeight={600} sx={{ mb: 2 }}>
                {t('invite:invite.shareToFriends')}
              </Typography>

              {/* Desktop: QR code + copy button + hint */}
              {!isMobile && (
                <>
                  <Box
                    sx={{
                      mb: 2,
                      display: "flex",
                      justifyContent: "center",
                    }}
                  >
                    <Box
                      sx={{
                        p: 1.5,
                        bgcolor: "white",
                        borderRadius: 2,
                        boxShadow: "0 2px 8px rgba(0,0,0,0.08)",
                      }}
                    >
                      {qrCodeLoading ? (
                        <Box
                          sx={{
                            width: 140,
                            height: 140,
                            display: "flex",
                            flexDirection: "column",
                            alignItems: "center",
                            justifyContent: "center",
                            bgcolor: "grey.100",
                          }}
                        >
                          <CircularProgress size={32} />
                          <Typography variant="caption" color="text.secondary" sx={{ mt: 1, textAlign: "center", px: 2 }}>
                            {t('invite:invite.generatingQR', '生成二维码中...')}
                          </Typography>
                        </Box>
                      ) : qrCodeError ? (
                        <Box
                          sx={{
                            width: 140,
                            height: 140,
                            display: "flex",
                            flexDirection: "column",
                            alignItems: "center",
                            justifyContent: "center",
                            bgcolor: "grey.100",
                            gap: 1,
                          }}
                        >
                          <QrCodeIcon sx={{ fontSize: 42, color: "error.main" }} />
                          <Typography variant="caption" color="error" sx={{ textAlign: "center", px: 1 }}>
                            {qrCodeError}
                          </Typography>
                          <Button
                            size="small"
                            variant="text"
                            onClick={generateQRCode}
                            sx={{ textTransform: "none", fontSize: "0.75rem" }}
                          >
                            {t('common:common.retry', '重试')}
                          </Button>
                        </Box>
                      ) : qrCodeUrl ? (
                        <img
                          src={qrCodeUrl}
                          alt="QR Code"
                          style={{
                            width: 140,
                            height: 140,
                            display: "block",
                          }}
                        />
                      ) : (
                        <Box
                          sx={{
                            width: 140,
                            height: 140,
                            display: "flex",
                            alignItems: "center",
                            justifyContent: "center",
                            bgcolor: "grey.100",
                          }}
                        >
                          <QrCodeIcon sx={{ fontSize: 42, color: "text.secondary" }} />
                        </Box>
                      )}
                    </Box>
                  </Box>

                  <Typography variant="caption" color="text.secondary" sx={{ display: "block", textAlign: "center", mb: 2 }}>
                    {t('invite:invite.scanQRToShare')}
                  </Typography>

                  <Button
                    variant="contained"
                    size="large"
                    fullWidth
                    data-tour="invite-copy"
                    startIcon={shareLinkLoading ? <CircularProgress size={20} color="inherit" /> : <CopyIcon />}
                    onClick={handleCopyShareContent}
                    disabled={shareLinkLoading}
                    sx={{
                      borderRadius: 2,
                      textTransform: "none",
                      fontWeight: 600,
                      py: 1.5,
                      boxShadow: (theme) => `0 4px 12px ${alpha(theme.palette.primary.main, 0.3)}`,
                    }}
                  >
                    {shareLinkLoading ? t('common:common.loading', '加载中...') : t('invite:invite.copyShareContent')}
                  </Button>
                </>
              )}

              {/* Mobile: share button only */}
              {isMobile && (
                <Button
                  variant="contained"
                  size="large"
                  fullWidth
                  data-tour="invite-share"
                  startIcon={shareLinkLoading ? <CircularProgress size={20} color="inherit" /> : <ShareIcon />}
                  onClick={handleCopyShareContent}
                  disabled={shareLinkLoading}
                  sx={{
                    borderRadius: 2,
                    textTransform: "none",
                    fontWeight: 600,
                    py: 1.5,
                    boxShadow: (theme) => `0 4px 12px ${alpha(theme.palette.primary.main, 0.3)}`,
                  }}
                >
                  {shareLinkLoading ? t('common:common.loading', '加载中...') : t('invite:invite.shareToFriends')}
                </Button>
              )}

              <Typography variant="caption" color="text.secondary" sx={{ display: "block", mt: 1.5 }}>
                {t('invite:invite.linkValid7Days')}
              </Typography>
            </Box>
          </Card>
        )}

        {/* Promotion Link Card */}
        {invite && (
          <Card
            elevation={0}
            sx={{
              mb: 2,
              borderRadius: 3,
              border: "1px solid",
              borderColor: "divider",
              overflow: "hidden",
            }}
          >
            <Box sx={{ p: 2.5 }}>
              <Typography variant="subtitle1" fontWeight={600} sx={{ mb: 2 }}>
                {t('invite:invite.promotionLink')}
              </Typography>

              <Paper
                elevation={0}
                sx={(theme) => ({
                  p: 1.5,
                  mb: 2,
                  borderRadius: 2,
                  bgcolor: theme.palette.mode === 'dark'
                    ? alpha(theme.palette.text.primary, 0.05)
                    : alpha(theme.palette.text.primary, 0.04),
                  border: "1px solid",
                  borderColor: "divider",
                })}
              >
                <Typography
                  variant="body2"
                  sx={{
                    fontFamily: "monospace",
                    wordBreak: "break-all",
                    color: "text.secondary",
                  }}
                >
                  {promotionLink}
                </Typography>
              </Paper>

              <Button
                variant="outlined"
                size="large"
                fullWidth
                startIcon={<LinkIcon />}
                onClick={handleCopyPromotionLink}
                sx={{
                  borderRadius: 2,
                  textTransform: "none",
                  fontWeight: 600,
                  py: 1.5,
                }}
              >
                {t('invite:invite.copyShareLink')}
              </Button>

              <Typography variant="caption" color="text.secondary" sx={{ display: "block", mt: 1.5 }}>
                {t('invite:invite.suitableForSocialMedia')}
              </Typography>
            </Box>
          </Card>
        )}

        {/* Invite rules for normal users */}
        {!user?.isRetailer && (
          <InviteRule invite={invite} loading={loadingInviteCode} />
        )}

        {/* Retailer stats */}
        <RetailerStatsOverview />
      </Box>
    </Box>
  );
}

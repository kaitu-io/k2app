/**
 * Tunnels - Self-hosted node management + cloud CTA
 *
 * Layout:
 * 1. Self-hosted node: always-visible URI input, save button
 * 2. Deploy guide: terminal-style block with curl command
 * 3. Cloud nodes: login CTA for guests, status for authenticated users
 */

import { useState, useCallback } from "react";
import {
  Box,
  Typography,
  Button,
  Stack,
  Paper,
  TextField,
  useTheme,
  IconButton,
  Tooltip,
} from "@mui/material";
import {
  Cloud as CloudIcon,
  ContentCopy as CopyIcon,
  Check as CheckIcon,
  Login as LoginIcon,
  Save as SaveIcon,
  OpenInNew as OpenInNewIcon,
  Terminal as TerminalIcon,
} from "@mui/icons-material";
import BackButton from "../components/BackButton";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "../stores";
import { useLoginDialogStore } from "../stores/login-dialog.store";
import { useSelfHostedStore, maskUriToken, parseK2v5Uri } from "../stores/self-hosted.store";
import { getThemeColors } from "../theme/colors";

const DEPLOY_COMMAND = 'curl -fsSL https://kaitu.io/i/k2s | sudo sh\nsudo k2s setup';

export default function Tunnels() {
  const { t } = useTranslation("dashboard");
  const theme = useTheme();
  const isDark = theme.palette.mode === "dark";
  const colors = getThemeColors(isDark);

  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const openLoginDialog = useLoginDialogStore((s) => s.open);

  const tunnel = useSelfHostedStore((s) => s.tunnel);
  const saveTunnel = useSelfHostedStore((s) => s.saveTunnel);
  const clearTunnel = useSelfHostedStore((s) => s.clearTunnel);

  // Input state — initialized from stored tunnel URI
  const [inputUri, setInputUri] = useState(tunnel?.uri ?? "");
  const [uriError, setUriError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [copied, setCopied] = useState(false);

  const storedUri = tunnel?.uri ?? "";
  const hasChanges = inputUri.trim() !== storedUri;

  const handleSave = useCallback(async () => {
    const trimmed = inputUri.trim();
    setUriError(null);

    // Empty = clear
    if (!trimmed) {
      setSaving(true);
      await clearTunnel();
      setSaving(false);
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
      return;
    }

    // Validate
    const result = parseK2v5Uri(trimmed);
    if (result.error) {
      setUriError(t(`selfHosted.${result.error}`));
      return;
    }

    setSaving(true);
    try {
      await saveTunnel(trimmed);
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    } catch (e: any) {
      setUriError(t(`selfHosted.${e.message}`));
    } finally {
      setSaving(false);
    }
  }, [inputUri, saveTunnel, clearTunnel, t]);

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(DEPLOY_COMMAND);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // fallback: try platform clipboard
      window._platform?.writeClipboard?.(DEPLOY_COMMAND);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  }, []);

  const handleOpenDocs = useCallback(() => {
    window._platform?.openExternal?.('https://kaitu.io/k2/server');
  }, []);

  const handleLogin = () => {
    openLoginDialog({
      trigger: "tunnels-page",
      message: t("tunnels.loginToSync"),
    });
  };

  // Display value: show masked token when input matches stored URI
  const displayValue = inputUri === storedUri && storedUri
    ? maskUriToken(storedUri)
    : inputUri;

  return (
    <Box sx={{ p: 2, pb: 6 }}>
      {/* Header */}
      <Box sx={{ display: "flex", alignItems: "center", mb: 2 }}>
        <BackButton />
        <Typography variant="h6" fontWeight={600} sx={{ ml: 1 }}>
          {t("tunnels.title")}
        </Typography>
      </Box>

      <Stack spacing={2}>
        {/* Self-Hosted Node */}
        <Paper
          elevation={0}
          sx={{
            p: 2.5,
            borderRadius: 2,
            bgcolor: isDark ? 'rgba(0, 212, 255, 0.05)' : 'rgba(0, 150, 255, 0.04)',
            border: `1px solid ${isDark ? 'rgba(0, 212, 255, 0.2)' : 'rgba(0, 150, 255, 0.15)'}`,
          }}
        >
          <Stack spacing={2}>
            <Stack direction="row" alignItems="center" spacing={1}>
              <TerminalIcon sx={{ fontSize: 20, color: colors.accent }} />
              <Typography variant="subtitle1" fontWeight={600}>
                {t("selfHosted.tag")}
              </Typography>
              {tunnel && (
                <Typography variant="caption" color="text.secondary">
                  {tunnel.country && `${tunnel.country} · `}{tunnel.name}
                </Typography>
              )}
            </Stack>

            <TextField
              size="small"
              fullWidth
              placeholder={t("selfHosted.inputPlaceholder")}
              value={displayValue}
              onChange={(e) => {
                setInputUri(e.target.value);
                setUriError(null);
                setSaved(false);
              }}
              onFocus={() => {
                // Show raw URI when focused for editing
                if (inputUri === storedUri && storedUri) {
                  setInputUri(storedUri);
                }
              }}
              error={!!uriError}
              helperText={uriError}
              InputProps={{
                sx: { fontFamily: 'monospace', fontSize: '0.85rem' },
              }}
            />

            <Button
              variant="contained"
              size="small"
              startIcon={saved ? <CheckIcon /> : <SaveIcon />}
              onClick={handleSave}
              disabled={!hasChanges || saving}
              sx={{ alignSelf: 'flex-start' }}
            >
              {saved ? t("selfHosted.saved") : t("selfHosted.save")}
            </Button>
          </Stack>
        </Paper>

        {/* Deploy Guide Terminal */}
        <Paper
          elevation={0}
          sx={{
            borderRadius: 2,
            overflow: 'hidden',
            border: `1px solid ${isDark ? 'rgba(255,255,255,0.1)' : 'rgba(0,0,0,0.1)'}`,
          }}
        >
          <Box sx={{ px: 2.5, py: 1.5 }}>
            <Typography variant="subtitle2" color="text.secondary">
              {t("selfHosted.deployGuide")}
            </Typography>
          </Box>

          <Box
            sx={{
              bgcolor: '#1a1a2e',
              px: 2.5,
              py: 2,
              fontFamily: 'monospace',
              fontSize: '0.82rem',
              lineHeight: 1.8,
              color: '#e0e0e0',
              position: 'relative',
            }}
          >
            <Box component="span" sx={{ color: '#4caf50' }}>$</Box>{' '}
            curl -fsSL https://kaitu.io/i/k2s | sudo sh{'\n'}
            <Box component="span" sx={{ color: '#4caf50' }}>$</Box>{' '}
            sudo k2s setup

            <Tooltip title={copied ? t("selfHosted.copied") : t("selfHosted.copyCommand")}>
              <IconButton
                size="small"
                onClick={handleCopy}
                sx={{
                  position: 'absolute',
                  top: 8,
                  right: 8,
                  color: 'rgba(255,255,255,0.5)',
                  '&:hover': { color: 'rgba(255,255,255,0.8)' },
                }}
              >
                {copied ? <CheckIcon fontSize="small" /> : <CopyIcon fontSize="small" />}
              </IconButton>
            </Tooltip>
          </Box>

          <Box sx={{ px: 2.5, py: 1.5, display: 'flex', justifyContent: 'flex-end' }}>
            <Button
              size="small"
              endIcon={<OpenInNewIcon sx={{ fontSize: '14px !important' }} />}
              onClick={handleOpenDocs}
              sx={{ textTransform: 'none', fontSize: '0.8rem' }}
            >
              {t("selfHosted.deployGuideDoc")}
            </Button>
          </Box>
        </Paper>

        {/* Cloud Nodes CTA */}
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
          {isAuthenticated ? (
            <Stack direction="row" spacing={1} alignItems="center" justifyContent="center">
              <CloudIcon sx={{ fontSize: 18, color: 'success.main' }} />
              <Typography variant="body2" color="text.secondary">
                {t("tunnels.cloudNodes.usingCloud")}
              </Typography>
            </Stack>
          ) : (
            <Stack spacing={1.5} alignItems="center">
              <CloudIcon sx={{ fontSize: 32, color: 'success.main' }} />
              <Typography variant="subtitle1" fontWeight={600}>
                {t("selfHosted.upgradeTitle")}
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ maxWidth: 320 }}>
                {t("selfHosted.upgradeDescription")}
              </Typography>
              <Button
                variant="contained"
                color="primary"
                size="medium"
                startIcon={<LoginIcon />}
                onClick={handleLogin}
                sx={{ px: 4, fontWeight: 600, borderRadius: 1.5 }}
              >
                {t("selfHosted.upgradeCta")}
              </Button>
            </Stack>
          )}
        </Paper>
      </Stack>
    </Box>
  );
}

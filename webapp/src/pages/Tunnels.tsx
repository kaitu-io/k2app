/**
 * Tunnels - Self-hosted node management
 *
 * Layout:
 * 1. Deploy command: copyable curl command for server setup
 * 2. Self-hosted node: URI input from deployed server
 * 3. Cloud hint: subtle login link for guests
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
  InputAdornment,
} from "@mui/material";
import {
  ContentCopy as CopyIcon,
  Check as CheckIcon,
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

const DEPLOY_COMMAND = 'curl -fsSL https://kaitu.io/i/k2s | sudo sh';

export default function Tunnels() {
  const { t } = useTranslation("dashboard");
  const { t: tc } = useTranslation("common");
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

  // Display value: show masked token when input matches stored URI
  const displayValue = inputUri === storedUri && storedUri
    ? maskUriToken(storedUri)
    : inputUri;

  const handleSave = useCallback(async () => {
    const trimmed = inputUri.trim();
    setUriError(null);

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
  }, [inputUri, saveTunnel, t]);

  const handleCopy = useCallback(async () => {
    await window._platform?.writeClipboard?.(DEPLOY_COMMAND);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }, []);

  const handleOpenDocs = useCallback(() => {
    window._platform?.openExternal?.('https://kaitu.io/k2/');
  }, []);

  const handleLogin = () => {
    openLoginDialog({
      trigger: "tunnels-page",
      message: t("tunnels.loginToSync"),
    });
  };

  return (
    <Box sx={{ p: 2 }}>
      {/* Header */}
      <Box sx={{ display: "flex", alignItems: "center", mb: 1.5 }}>
        <BackButton />
        <Typography variant="h6" fontWeight={600} sx={{ ml: 1 }}>
          {t("tunnels.title")}
        </Typography>
      </Box>

      <Stack spacing={1.5}>
        {/* Deploy Command */}
        <Paper
          elevation={0}
          sx={{
            p: 2,
            borderRadius: 2,
            bgcolor: colors.accentBgLighter,
            border: `1px solid ${colors.accentBorder}`,
          }}
        >
          <Stack spacing={1.5}>
            <Stack direction="row" alignItems="center" spacing={1}>
              <TerminalIcon sx={{ fontSize: 18, color: colors.accent }} />
              <Typography variant="subtitle2" fontWeight={600}>
                {t("selfHosted.deployGuide")}
              </Typography>
            </Stack>

            <TextField
              size="small"
              fullWidth
              value={DEPLOY_COMMAND}
              InputProps={{
                readOnly: true,
                sx: { fontFamily: 'monospace', fontSize: '0.82rem' },
                endAdornment: (
                  <InputAdornment position="end">
                    <Tooltip title={copied ? t("selfHosted.copied") : t("selfHosted.copyCommand")}>
                      <IconButton
                        size="small"
                        onClick={handleCopy}
                        edge="end"
                      >
                        {copied ? <CheckIcon fontSize="small" /> : <CopyIcon fontSize="small" />}
                      </IconButton>
                    </Tooltip>
                  </InputAdornment>
                ),
              }}
              onClick={(e) => {
                // Select all text on click for easy manual copy
                const input = e.currentTarget.querySelector('input');
                input?.select();
              }}
            />

            <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
              <Button
                size="small"
                endIcon={<OpenInNewIcon sx={{ fontSize: '14px !important' }} />}
                onClick={handleOpenDocs}
                sx={{ textTransform: 'none', fontSize: '0.8rem' }}
              >
                {t("selfHosted.deployGuideDoc")}
              </Button>
            </Box>
          </Stack>
        </Paper>

        {/* Self-Hosted Node URI */}
        <Paper
          elevation={0}
          sx={{
            p: 2,
            borderRadius: 2,
            bgcolor: colors.accentBgLighter,
            border: `1px solid ${colors.accentBorder}`,
          }}
        >
          <Stack spacing={1.5}>
            <Stack direction="row" alignItems="center" spacing={1}>
              <Typography variant="subtitle2" fontWeight={600}>
                {t("selfHosted.tag")}
              </Typography>
              {tunnel && (
                <Typography variant="caption" color="text.secondary">
                  {tunnel.country && `${tunnel.country} · `}{tunnel.name}
                </Typography>
              )}
            </Stack>

            <Typography variant="body2" color="text.secondary" sx={{ mt: -0.5 }}>
              {t("selfHosted.uriHelp")}
            </Typography>

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
                sx: { fontFamily: 'monospace', fontSize: '0.82rem' },
              }}
            />

            <Stack direction="row" spacing={1} justifyContent="flex-end">
              {tunnel && (
                <Button
                  size="small"
                  variant="text"
                  onClick={async () => {
                    await clearTunnel();
                    setInputUri("");
                  }}
                >
                  {tc('common.clear')}
                </Button>
              )}
              <Button
                variant="contained"
                size="small"
                startIcon={saved ? <CheckIcon /> : <SaveIcon />}
                onClick={handleSave}
                disabled={!hasChanges || saving}
              >
                {saved ? t("selfHosted.saved") : t("selfHosted.save")}
              </Button>
            </Stack>
          </Stack>
        </Paper>

        {/* Cloud hint for guests */}
        {!isAuthenticated && (
          <Typography variant="body2" color="text.secondary" sx={{ textAlign: 'center', pt: 0.5 }}>
            {t("selfHosted.upgradeTitle")}{' '}
            <Button
              size="small"
              onClick={handleLogin}
              sx={{ textTransform: 'none', fontWeight: 600, minWidth: 0 }}
            >
              {t("selfHosted.upgradeCta")}
            </Button>
          </Typography>
        )}
      </Stack>
    </Box>
  );
}

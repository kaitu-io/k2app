import { useState, useEffect } from "react";
import { useNavigate, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import {
  Box,
  Card,
  CardContent,
  Typography,
  TextField,
  Button,
  Stack,
  CircularProgress,
  Divider,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogContentText,
  DialogActions,
} from "@mui/material";

import BackButton from "../components/BackButton";
import { cloudApi } from "../services/cloud-api";
import { useAlert } from "../stores";
import { getErrorMessage } from "../utils/errorCode";

interface DelegateInfo {
  email: string;
  setAt: number;
}

function isDelegateInfo(data: unknown): data is DelegateInfo {
  return (
    !!data &&
    typeof data === "object" &&
    "email" in data &&
    typeof (data as { email: unknown }).email === "string" &&
    !!(data as { email: string }).email
  );
}

export default function Delegate() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const { showAlert } = useAlert();

  const returnTo = new URLSearchParams(location.search).get("returnTo");

  const [loading, setLoading] = useState(true);
  const [delegate, setDelegate] = useState<DelegateInfo | null>(null);
  const [editing, setEditing] = useState(false);
  const [email, setEmail] = useState("");
  const [saving, setSaving] = useState(false);
  const [removeConfirmOpen, setRemoveConfirmOpen] = useState(false);

  useEffect(() => {
    (async () => {
      try {
        const response = await cloudApi.get<DelegateInfo | Record<string, never>>(
          "/api/user/delegate"
        );
        if (response?.code === 0 && isDelegateInfo(response.data)) {
          setDelegate(response.data);
          setEditing(false);
        } else {
          setDelegate(null);
          setEditing(true);
        }
      } catch (err) {
        console.error("[Delegate] Failed to load delegate:", err);
        setDelegate(null);
        setEditing(true);
      } finally {
        setLoading(false);
      }
    })();
  }, []);

  const handleSave = async () => {
    const trimmed = email.trim();
    if (!trimmed) return;

    setSaving(true);
    try {
      const response = await cloudApi.request<DelegateInfo>(
        "PUT",
        "/api/user/delegate",
        { email: trimmed }
      );
      if (response?.code === 0 && response.data) {
        setDelegate(response.data);
        setEditing(false);
        setEmail("");
        showAlert(t("account:delegate.savedToast"), "success");
        if (returnTo) {
          navigate(returnTo);
        }
      } else {
        console.warn("[Delegate] save failed:", response?.code, response?.message);
        showAlert(
          getErrorMessage(response?.code ?? -1, response?.message, t, t("account:delegate.saveFailed")),
          "error"
        );
      }
    } catch (err) {
      console.error("[Delegate] Failed to save delegate:", err);
      showAlert(t("account:delegate.saveFailed"), "error");
    } finally {
      setSaving(false);
    }
  };

  const handleRemove = async () => {
    setRemoveConfirmOpen(false);
    setSaving(true);
    try {
      const response = await cloudApi.request("DELETE", "/api/user/delegate");
      if (response?.code === 0) {
        setDelegate(null);
        setEditing(true);
      } else {
        console.warn("[Delegate] remove failed:", response?.code, response?.message);
        showAlert(
          getErrorMessage(response?.code ?? -1, response?.message, t, t("account:delegate.removeFailed")),
          "error"
        );
      }
    } catch (err) {
      console.error("[Delegate] Failed to remove delegate:", err);
      showAlert(t("account:delegate.removeFailed"), "error");
    } finally {
      setSaving(false);
    }
  };

  const showForm = !delegate || editing;

  return (
    <Box sx={{ width: "100%", py: 0.25, position: "relative" }}>
      <BackButton to="/account" />
      <Box sx={{ display: "flex", alignItems: "center", mb: 1.5, px: 0.75, pt: 7 }}>
        <Typography variant="body1" sx={{ flex: 1, fontWeight: 600 }} component="span">
          {t("account:delegate.pageTitle")}
        </Typography>
      </Box>

      {loading ? (
        <Box sx={{ display: "flex", justifyContent: "center", py: 4 }}>
          <CircularProgress size={28} />
        </Box>
      ) : (
        <Card sx={{ mx: 0.75 }}>
          <CardContent>
            {showForm ? (
              <Stack spacing={2}>
                <Typography variant="body2" color="text.secondary">
                  {t("account:delegate.emptyDescription")}
                </Typography>
                <TextField
                  label={t("account:delegate.emailLabel")}
                  type="email"
                  placeholder={t("account:delegate.emailPlaceholder")}
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  disabled={saving}
                  fullWidth
                  size="small"
                  autoFocus
                />
                <Stack direction="row" spacing={1}>
                  <Button
                    variant="contained"
                    onClick={handleSave}
                    disabled={saving || !email.trim()}
                    startIcon={saving ? <CircularProgress size={16} color="inherit" /> : undefined}
                  >
                    {t("account:delegate.saveButton")}
                  </Button>
                  {delegate && (
                    <Button
                      variant="text"
                      onClick={() => {
                        setEditing(false);
                        setEmail("");
                      }}
                      disabled={saving}
                    >
                      {t("account:delegate.cancelButton")}
                    </Button>
                  )}
                </Stack>
                <Typography variant="caption" color="text.secondary">
                  {t("account:delegate.emptyHint")}
                </Typography>
              </Stack>
            ) : (
              <Stack spacing={2}>
                <Box
                  sx={{
                    borderRadius: 1.5,
                    p: 2,
                    backgroundColor: (theme) =>
                      theme.palette.mode === "dark"
                        ? "rgba(255,255,255,0.04)"
                        : "rgba(0,0,0,0.03)",
                  }}
                >
                  <Typography variant="caption" color="text.secondary" display="block">
                    {t("account:delegate.currentTitle")}
                  </Typography>
                  <Typography
                    variant="h6"
                    sx={{ fontWeight: 700, mt: 0.5, wordBreak: "break-all" }}
                  >
                    {delegate!.email}
                  </Typography>
                  <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5, display: "block" }}>
                    {t("account:delegate.setAtLabel", {
                      date: new Date(delegate!.setAt * 1000).toLocaleDateString(),
                    })}
                  </Typography>
                </Box>
                <Stack direction="row" spacing={1}>
                  <Button
                    variant="outlined"
                    onClick={() => setEditing(true)}
                    disabled={saving}
                  >
                    {t("account:delegate.modifyButton")}
                  </Button>
                  <Button
                    variant="outlined"
                    color="error"
                    onClick={() => setRemoveConfirmOpen(true)}
                    disabled={saving}
                    startIcon={saving ? <CircularProgress size={16} color="inherit" /> : undefined}
                  >
                    {t("account:delegate.removeButton")}
                  </Button>
                </Stack>
                <Divider />
                <Typography variant="caption" color="text.secondary">
                  {t("account:delegate.currentHint", { email: delegate!.email })}
                </Typography>
              </Stack>
            )}
          </CardContent>
        </Card>
      )}

      <Dialog
        open={removeConfirmOpen}
        onClose={() => !saving && setRemoveConfirmOpen(false)}
      >
        <DialogTitle>{t("account:delegate.removeButton")}</DialogTitle>
        <DialogContent>
          <DialogContentText>
            {t("account:delegate.removeConfirm")}
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button
            onClick={() => setRemoveConfirmOpen(false)}
            disabled={saving}
          >
            {t("common:common.cancel")}
          </Button>
          <Button
            onClick={handleRemove}
            color="error"
            disabled={saving}
            startIcon={saving ? <CircularProgress size={16} color="inherit" /> : undefined}
          >
            {t("common:common.confirm")}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}

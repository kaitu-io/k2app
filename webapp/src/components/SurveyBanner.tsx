import { useState, useEffect } from "react";
import { Box, Typography, Button, IconButton } from "@mui/material";
import CloseIcon from "@mui/icons-material/Close";
import CardGiftcardIcon from "@mui/icons-material/CardGiftcard";
import { useTranslation } from "react-i18next";
import { cloudApi } from "../services/cloud-api";
import { useUser } from "../hooks/useUser";

const SURVEY_KEY = "active_2026q1";
const CONNECT_COUNT_KEY = "k2_connect_success_count";
const DISMISS_KEY = `survey_dismissed_${SURVEY_KEY}`;
const CONNECT_THRESHOLD = 5;

const SurveyBanner: React.FC = () => {
  const { t } = useTranslation();
  const { user, isMembership } = useUser();
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    if (!isMembership || !user?.uuid) return;
    if (localStorage.getItem(DISMISS_KEY) === "true") return;

    const count = parseInt(localStorage.getItem(CONNECT_COUNT_KEY) || "0", 10);
    if (count < CONNECT_THRESHOLD) return;

    // Check server-side status via cloudApi
    const checkStatus = async () => {
      try {
        const { code, data } = await cloudApi.get<{ submitted: boolean }>(
          `/api/survey/status?survey_key=${SURVEY_KEY}`
        );
        if (code === 0 && !data?.submitted) {
          setVisible(true);
        }
      } catch {
        // Fail silently
      }
    };
    checkStatus();
  }, [isMembership, user?.uuid]);

  const handleDismiss = () => {
    localStorage.setItem(DISMISS_KEY, "true");
    setVisible(false);
  };

  const handleClick = () => {
    const url = `https://kaitu.io/survey/${SURVEY_KEY}`;
    if (window._platform?.openExternal) {
      window._platform.openExternal(url);
    } else {
      window.open(url, "_blank");
    }
    handleDismiss();
  };

  if (!visible) return null;

  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "center",
        gap: 1,
        px: 2,
        py: 1,
        bgcolor: "primary.dark",
        borderRadius: 1,
        mb: 1,
      }}
    >
      <CardGiftcardIcon sx={{ fontSize: 20, color: "warning.main" }} />
      <Typography variant="body2" sx={{ flex: 1, color: "common.white" }}>
        {t("survey.banner_text", "\u586b\u5199 1 \u5206\u949f\u95ee\u5377\uff0c\u514d\u8d39\u9886\u53d6 1 \u4e2a\u6708\u4f7f\u7528\u6743")}
      </Typography>
      <Button size="small" variant="contained" color="warning" onClick={handleClick}>
        {t("survey.banner_cta", "\u7acb\u5373\u586b\u5199")}
      </Button>
      <IconButton size="small" onClick={handleDismiss} sx={{ color: "common.white" }}>
        <CloseIcon fontSize="small" />
      </IconButton>
    </Box>
  );
};

export default SurveyBanner;

import { Box, Typography, Button } from "@mui/material";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";

import BackButton from "../components/BackButton";
import BindEmailForm from "../components/BindEmailForm";

export default function UpdateLoginEmail() {
  const navigate = useNavigate();
  const { t } = useTranslation();

  return (
    <Box sx={{
      width: "100%",
      py: 0.5,
      backgroundColor: "transparent",
      position: "relative"
    }}>
      <BackButton to="/account" />
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 1.5, px: 1, pt: 7 }}>
        <Typography variant="h6" sx={{ flex: 1, fontWeight: 600 }} component="span">
          {t('auth:updateEmail.title')}
        </Typography>
      </Box>
      <Box sx={{ width: "100%", bgcolor: 'background.paper', p: 2, borderRadius: 2 }}>
        <BindEmailForm
          autoFocus
          onBound={() => navigate("/account", { replace: true })}
          renderSecondaryAction={(busy) => (
            <Button
              variant="outlined"
              onClick={() => navigate("/account")}
              disabled={busy}
              size="small"
            >
              {t('common:common.cancel')}
            </Button>
          )}
        />
      </Box>
    </Box>
  );
}

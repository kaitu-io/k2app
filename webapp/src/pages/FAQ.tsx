import { useNavigate, useLocation } from "react-router-dom";
import {
  Box,
  Typography,
  Accordion,
  AccordionSummary,
  AccordionDetails,
  Button,
  Stack,
} from "@mui/material";
import {
  ExpandMore as ExpandMoreIcon,
  SupportAgent as SupportIcon,
} from "@mui/icons-material";
import { useTranslation } from "react-i18next";
import BackButton from "../components/BackButton";
import { FAQ_KEYS } from "./faq-items";

export default function FAQ() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();

  const backTo = (location.state as { from?: string })?.from || "/feedback";

  return (
    <Box sx={{ width: "100%", height: "100%", position: "relative" }}>
      <BackButton to={backTo} />

      <Box
        sx={{
          width: "100%",
          height: "100%",
          display: "flex",
          justifyContent: "center",
          alignItems: "flex-start",
          pt: 9,
        }}
      >
        <Box
          sx={{
            width: 560,
            maxWidth: "100%",
            display: "flex",
            flexDirection: "column",
            gap: 1,
            overflow: "auto",
            height: "100%",
            px: 2,
            pb: 4,
          }}
        >
          <Typography variant="h6" sx={{ mb: 1 }}>
            {t("ticket:faq.title")}
          </Typography>

          <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
            {t("ticket:faq.subtitle")}
          </Typography>

          {FAQ_KEYS.map((key) => (
            <Accordion
              key={key}
              disableGutters
              sx={{
                bgcolor: "background.paper",
                "&:before": { display: "none" },
                borderRadius: 1,
                mb: 0.5,
              }}
            >
              <AccordionSummary expandIcon={<ExpandMoreIcon />}>
                <Typography variant="body2" sx={{ fontWeight: 500 }}>
                  {t(`ticket:faq.items.${key}.question`)}
                </Typography>
              </AccordionSummary>
              <AccordionDetails>
                <Typography
                  variant="body2"
                  color="text.secondary"
                  sx={{ whiteSpace: "pre-line", lineHeight: 1.7 }}
                >
                  {t(`ticket:faq.items.${key}.answer`)}
                </Typography>
              </AccordionDetails>
            </Accordion>
          ))}

          <Stack spacing={1} sx={{ mt: 3, alignItems: "center" }}>
            <Typography variant="body2" color="text.secondary">
              {t("ticket:faq.stillNeedHelp")}
            </Typography>
            <Button
              variant="contained"
              startIcon={<SupportIcon />}
              onClick={() => navigate("/submit-ticket-form")}
            >
              {t("ticket:faq.submitCta")}
            </Button>
          </Stack>
        </Box>
      </Box>
    </Box>
  );
}

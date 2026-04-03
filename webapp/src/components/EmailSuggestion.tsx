import { Typography, Link, Box } from "@mui/material";
import { useTranslation } from "react-i18next";

interface EmailSuggestionProps {
  suggestion: string;
  onAccept: () => void;
}

export default function EmailSuggestion({ suggestion, onAccept }: EmailSuggestionProps) {
  const { t } = useTranslation();

  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
      <Typography component="span" variant="caption" color="warning.main">
        {t("auth:auth.emailTypoSuggestion", { suggested: suggestion })}
      </Typography>
      <Link
        component="button"
        type="button"
        variant="caption"
        onClick={onAccept}
        sx={{ fontWeight: 600, cursor: 'pointer' }}
      >
        {t("auth:auth.emailTypoUseSuggestion", "Use suggestion")}
      </Link>
    </Box>
  );
}

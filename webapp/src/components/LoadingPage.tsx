import React from "react";
import { Box, CircularProgress, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";

const LoadingPage: React.FC = () => {
  const { t } = useTranslation();
  
  return (
    <Box
      sx={{
        display: "flex",
        flexDirection: "column",
        justifyContent: "center",
        alignItems: "center",
        height: "100vh",
        backgroundColor: "background.default",
      }}
    >
      <CircularProgress size={60} />
      <Typography variant="h6" sx={{ mt: 2 }}>
        {t('common:common.loading')}
      </Typography>
    </Box>
  );
};

export default LoadingPage; 
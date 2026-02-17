import { IconButton } from "@mui/material";
import { ArrowBack as ArrowBackIcon } from "@mui/icons-material";
import { useNavigate } from "react-router-dom";

interface BackButtonProps {
  to?: string;
  onClick?: () => void;
}

export default function BackButton({ to, onClick }: BackButtonProps) {
  const navigate = useNavigate();

  const handleClick = () => {
    if (onClick) {
      onClick();
    } else if (to) {
      navigate(to);
    } else {
      navigate(-1);
    }
  };

  return (
    <IconButton
      onClick={handleClick}
      sx={{
        position: 'absolute',
        left: 8,
        top: 16,
        zIndex: 10,
        bgcolor: (theme) => theme.palette.mode === 'dark'
          ? 'rgba(255, 255, 255, 0.08)'
          : 'rgba(0, 0, 0, 0.04)',
        '&:hover': {
          bgcolor: (theme) => theme.palette.mode === 'dark'
            ? 'rgba(255, 255, 255, 0.12)'
            : 'rgba(0, 0, 0, 0.08)',
        },
        transition: 'all 0.2s',
      }}
    >
      <ArrowBackIcon />
    </IconButton>
  );
}

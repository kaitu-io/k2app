import { Box } from "@mui/material";
import { getThemeColors } from "../theme/colors";

interface HighlightedTextProps {
  text: string;
  component?: React.ElementType;
}

/**
 * HighlightedText Component
 *
 * Renders text with highlighted sections marked by ##markers##
 * Uses centralized theme colors from colors.ts
 *
 * Usage:
 *   <HighlightedText text="Regular text ##highlighted## more text" />
 *   <HighlightedText text={t('some.key')} component="span" />
 */
export const HighlightedText: React.FC<HighlightedTextProps> = ({
  text,
  component = "span"
}) => {
  if (!text) return null;

  const parts = text.split('##');

  return (
    <Box component={component}>
      {parts.map((part, index) => {
        // Odd indices are the highlighted sections
        if (index % 2 === 1 && part.trim()) {
          return (
            <Box
              key={index}
              component="span"
              sx={(theme) => {
                const colors = getThemeColors(theme.palette.mode === 'dark');
                return {
                  color: colors.highlightColor,
                  fontWeight: 700,
                  background: colors.highlightBg,
                  px: 0.75,
                  py: 0.25,
                  borderRadius: 0.75,
                  display: 'inline-block',
                };
              }}
            >
              {part}
            </Box>
          );
        }
        return part ? <span key={index}>{part}</span> : null;
      })}
    </Box>
  );
};

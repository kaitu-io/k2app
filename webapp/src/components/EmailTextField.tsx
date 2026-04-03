// webapp/src/components/EmailTextField.tsx
import { TextField, TextFieldProps, Typography, Link, Box } from "@mui/material";
import { forwardRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { suggestEmail } from "../utils/email-suggest";

interface EmailTextFieldProps extends Omit<TextFieldProps, 'type' | 'onChange' | 'error' | 'helperText'> {
  value: string;
  onChange: (cleanEmail: string) => void;
  helperText?: string;
}

const EmailTextField = forwardRef<HTMLDivElement, EmailTextFieldProps>(
  ({ value, onChange, helperText, ...props }, ref) => {
    const { t } = useTranslation();
    const [error, setError] = useState(false);
    const [errorMessage, setErrorMessage] = useState<string>("");
    const [suggestion, setSuggestion] = useState<string | null>(null);

    const validateEmail = (email: string): boolean => {
      const emailRegex = /^[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}$/i;
      return emailRegex.test(email);
    };

    const cleanEmailInput = (inputValue: string): string => {
      return inputValue
        .trim()
        .toLowerCase()
        .replace(/\s+/g, '');
    };

    const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
      onChange(e.target.value);
      if (error) {
        setError(false);
        setErrorMessage("");
      }
      if (suggestion) {
        setSuggestion(null);
      }
    };

    const handleBlur = (e: React.FocusEvent<HTMLInputElement>) => {
      const cleanValue = cleanEmailInput(e.target.value);
      onChange(cleanValue);

      if (cleanValue && !validateEmail(cleanValue)) {
        setError(true);
        setErrorMessage(t("auth:auth.invalidEmailFormat", "Please enter a valid email address"));
        setSuggestion(null);
      } else if (cleanValue) {
        const suggested = suggestEmail(cleanValue);
        setSuggestion(suggested);
      }

      if (props.onBlur) {
        props.onBlur(e);
      }
    };

    const handleUseSuggestion = () => {
      if (suggestion) {
        onChange(suggestion);
        setSuggestion(null);
      }
    };

    const renderHelperText = () => {
      if (error) return errorMessage;
      if (suggestion) {
        return (
          <Box component="span" sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5 }}>
            <Typography component="span" variant="caption" color="warning.main">
              {t("auth:auth.emailTypoSuggestion", { suggested: suggestion })}
            </Typography>
            <Link
              component="button"
              type="button"
              variant="caption"
              onClick={handleUseSuggestion}
              sx={{ fontWeight: 600, cursor: 'pointer' }}
            >
              {t("auth:auth.emailTypoUseSuggestion", "Use suggestion")}
            </Link>
          </Box>
        );
      }
      return helperText;
    };

    return (
      <TextField
        {...props}
        ref={ref}
        type="email"
        value={value}
        onChange={handleChange}
        onBlur={handleBlur}
        error={error}
        helperText={renderHelperText()}
        inputProps={{
          maxLength: 100,
          autoCapitalize: "none",
          autoCorrect: "off",
          autoComplete: "email",
          spellCheck: false,
          ...props.inputProps,
        }}
      />
    );
  }
);

EmailTextField.displayName = 'EmailTextField';

export default EmailTextField;

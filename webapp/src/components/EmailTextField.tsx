import { TextField, TextFieldProps } from "@mui/material";
import { forwardRef, useState } from "react";
import { useTranslation } from "react-i18next";

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

    const validateEmail = (email: string): boolean => {
      const emailRegex = /^[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}$/i;
      return emailRegex.test(email);
    };

    // Clean email on blur only - avoid real-time transformations
    // that can cause input issues on old WebViews
    const cleanEmailInput = (inputValue: string): string => {
      return inputValue
        .trim()
        .toLowerCase()
        .replace(/\s+/g, '');
    };

    const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
      // Pass raw value to parent - no transformation during typing
      // This avoids controlled component desync on old WebViews
      onChange(e.target.value);

      // Clear error while typing
      if (error) {
        setError(false);
        setErrorMessage("");
      }
    };

    const handleBlur = (e: React.FocusEvent<HTMLInputElement>) => {
      // Clean and validate on blur only
      const cleanValue = cleanEmailInput(e.target.value);
      onChange(cleanValue);

      // Validate on blur
      if (cleanValue && !validateEmail(cleanValue)) {
        setError(true);
        setErrorMessage(t("auth:auth.invalidEmailFormat", "Please enter a valid email address"));
      }

      if (props.onBlur) {
        props.onBlur(e);
      }
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
        helperText={error ? errorMessage : helperText}
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
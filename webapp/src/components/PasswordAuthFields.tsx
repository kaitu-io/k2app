import { useEffect, useRef } from 'react';
import {
  TextField,
  Button,
  InputAdornment,
  CircularProgress,
} from '@mui/material';
import { AlternateEmail as AlternateEmailIcon, Lock as LockIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import EmailSuggestion from './EmailSuggestion';
import { delayedFocus } from '../utils/ui';

export interface PasswordAuthFieldsProps {
  email: string;
  password: string;
  onEmailChange: (v: string) => void;
  onPasswordChange: (v: string) => void;
  onSubmit: () => void;
  onEmailBlur: () => void;
  emailSuggestion: string | null;
  onAcceptSuggestion: () => void;
  isSubmitting: boolean;
  /** Auto-focus the email field on mount. Default true. */
  autoFocusEmail?: boolean;
}

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export default function PasswordAuthFields(props: PasswordAuthFieldsProps) {
  const { t } = useTranslation();
  const emailRef = useRef<HTMLInputElement>(null);

  const emailValid = props.email.trim() !== '' && EMAIL_RE.test(props.email);
  const canSubmit = emailValid && props.password.length > 0 && !props.isSubmitting;

  useEffect(() => {
    if (props.autoFocusEmail === false) return;
    return delayedFocus(() => emailRef.current, 100);
  }, [props.autoFocusEmail]);

  const handleEnter = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' && canSubmit) {
      props.onSubmit();
    }
  };

  return (
    <>
      <TextField
        fullWidth
        label={t('auth:auth.email')}
        placeholder={t('auth:auth.emailPlaceholder')}
        value={props.email}
        onChange={(e) => props.onEmailChange(e.target.value)}
        onBlur={props.onEmailBlur}
        onKeyDown={handleEnter}
        disabled={props.isSubmitting}
        inputRef={emailRef}
        inputProps={{
          autoCapitalize: 'none',
          autoCorrect: 'off',
          autoComplete: 'email',
          spellCheck: false,
        }}
        InputProps={{
          startAdornment: (
            <InputAdornment position="start">
              <AlternateEmailIcon color="primary" />
            </InputAdornment>
          ),
        }}
        sx={{ '& .MuiOutlinedInput-root': { borderRadius: 2 } }}
      />

      {props.emailSuggestion && (
        <EmailSuggestion
          suggestion={props.emailSuggestion}
          onAccept={props.onAcceptSuggestion}
        />
      )}

      <TextField
        fullWidth
        type="password"
        label={t('auth:auth.password')}
        placeholder={t('auth:auth.passwordPlaceholder')}
        value={props.password}
        onChange={(e) => props.onPasswordChange(e.target.value)}
        onKeyDown={handleEnter}
        disabled={props.isSubmitting}
        inputProps={{
          autoCapitalize: 'none',
          autoCorrect: 'off',
          autoComplete: 'current-password',
          spellCheck: false,
        }}
        InputProps={{
          startAdornment: (
            <InputAdornment position="start">
              <LockIcon color="primary" />
            </InputAdornment>
          ),
        }}
        sx={{ '& .MuiOutlinedInput-root': { borderRadius: 2 } }}
      />

      <Button
        fullWidth
        size="large"
        variant="contained"
        onClick={props.onSubmit}
        disabled={!canSubmit}
        startIcon={props.isSubmitting ? <CircularProgress size={20} color="inherit" /> : null}
        sx={{
          py: 1.5,
          borderRadius: 2,
          textTransform: 'none',
          fontSize: '1rem',
          fontWeight: 600,
        }}
      >
        {t('auth:auth.login')}
      </Button>
    </>
  );
}

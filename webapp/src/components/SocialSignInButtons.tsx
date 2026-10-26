/**
 * SocialSignInButtons — "Continue with Apple / Google" under the email form.
 *
 * Used by LoginDialog and EmailLoginForm. Owns the whole flow (native sheet →
 * /api/auth/social/login → authenticated); the parent only reacts to success
 * and shows errors in its own Alert. Renders nothing when no provider is
 * usable, so old shells, the router UI and switched-off brands see no change.
 *
 * `mode="bind"`: the user is already signed in (a zero-identity device account
 * on 【其他设备安装】) and adds this provider as a login identity via
 * /api/user/social/bind. Not a login — no auth_done, no isAuthenticated change,
 * no device fields in the body. The "or" divider goes below the buttons there,
 * because the email bind form follows them.
 */
import { useEffect, useState } from 'react';
import { Button, CircularProgress, Divider, Stack, Typography, useTheme } from '@mui/material';
import { Apple as AppleIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useAuthStore } from '../stores';
import { useAppConfig } from '../hooks/useAppConfig';
import { cloudApi } from '../services/cloud-api';
import { cacheStore } from '../services/cache-store';
import { getDeviceUdid } from '../services/device-udid';
import { statsService } from '../services/stats';
import { createNonce, resolveSocialProviders, sha256Hex } from '../services/social-sign-in';
import { handleResponseError } from '../utils/errorCode';
import type { AuthResult } from '../services/api-types';
import type { ISocialSignIn, SocialCredential, SocialProvider } from '../types/kaitu-core';

export interface SocialSignInButtonsProps {
  onSuccess: () => void;
  onError: (message: string) => void;
  /** Parent is mid-request (e.g. sending a code) — don't start a second one. */
  disabled?: boolean;
  /** 'login' (default): sign in. 'bind': add the identity to the signed-in account. */
  mode?: 'login' | 'bind';
}

/** Google's multi-colour "G" (brand guidelines forbid a monochrome mark). */
function GoogleGlyph() {
  return (
    <svg width="18" height="18" viewBox="0 0 48 48" aria-hidden="true">
      <path fill="#EA4335" d="M24 9.5c3.54 0 6.71 1.22 9.21 3.6l6.85-6.85C35.9 2.38 30.47 0 24 0 14.62 0 6.51 5.38 2.56 13.22l7.98 6.19C12.43 13.72 17.74 9.5 24 9.5z" />
      <path fill="#4285F4" d="M46.98 24.55c0-1.57-.15-3.09-.38-4.55H24v9.02h12.94c-.58 2.96-2.26 5.48-4.78 7.18l7.73 6c4.51-4.18 7.09-10.36 7.09-17.65z" />
      <path fill="#FBBC05" d="M10.53 28.59c-.48-1.45-.76-2.99-.76-4.59s.27-3.14.76-4.59l-7.98-6.19C.92 16.46 0 20.12 0 24c0 3.88.92 7.54 2.56 10.78l7.97-6.19z" />
      <path fill="#34A853" d="M24 48c6.48 0 11.93-2.13 15.89-5.81l-7.73-6c-2.15 1.45-4.92 2.3-8.16 2.3-6.26 0-11.57-4.22-13.47-9.91l-7.98 6.19C6.51 42.62 14.62 48 24 48z" />
    </svg>
  );
}

function SocialSignInInner({ bridge, onSuccess, onError, disabled, mode = 'login' }: SocialSignInButtonsProps & { bridge: ISocialSignIn }) {
  const { t, i18n } = useTranslation();
  const isDark = useTheme().palette.mode === 'dark';
  const { appConfig } = useAppConfig();
  const setIsAuthenticated = useAuthStore((s) => s.setIsAuthenticated);
  const [providers, setProviders] = useState<SocialProvider[]>([]);
  const [busy, setBusy] = useState<SocialProvider | null>(null);

  const serverSwitch = appConfig?.socialSignIn;
  const appleOn = serverSwitch?.apple ?? false;
  const googleOn = serverSwitch?.google ?? false;

  // Re-resolved per mount (= per dialog open): the network can change between
  // opens, e.g. the user connects the VPN and Google becomes reachable.
  useEffect(() => {
    let cancelled = false;
    void resolveSocialProviders({
      serverSwitch: serverSwitch ? { apple: appleOn, google: googleOn } : undefined,
      onImmediate: (p) => { if (!cancelled) setProviders(p); },
    }).then((p) => { if (!cancelled) setProviders(p); });
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- inputs are the two booleans
  }, [Boolean(serverSwitch), appleOn, googleOn]);

  const handleSignIn = async (provider: SocialProvider) => {
    setBusy(provider);
    try {
      const nonce = createNonce();
      let credential: SocialCredential | null;
      try {
        credential = await bridge.signIn(provider, await sha256Hex(nonce));
      } catch (err) {
        // Native errors are English SDK strings — log them, show our own text.
        console.error(`[SocialSignIn] ${provider} native sign-in failed:`, err);
        onError(t('auth:auth.socialSignInFailed'));
        return;
      }
      if (!credential) return; // user cancelled

      if (mode === 'bind') {
        // api/type.go DataSocialBindRequest: provider, idToken, nonce, authorizationCode.
        const response = await cloudApi.post('/api/user/social/bind', {
          provider,
          idToken: credential.idToken,
          nonce,
          authorizationCode: credential.authorizationCode,
        });
        handleResponseError(response.code, response.message, t, t('account:deviceBind.bindFailed'));
        // The identity list on /api/user/info changed; drop cached copies.
        cacheStore.clear();
        onSuccess();
        return;
      }

      const response = await cloudApi.post<AuthResult>('/api/auth/social/login', {
        provider,
        idToken: credential.idToken,
        nonce,
        authorizationCode: credential.authorizationCode,
        udid: await getDeviceUdid(),
        remark: t('startup:startup.newDevice'),
        language: i18n.language,
      });
      handleResponseError(response.code, response.message, t, t('auth:auth.loginFailed'));

      // Tokens are saved by cloudApi (AUTH_TOKEN_PATHS); same tail as code login.
      cacheStore.clear();
      setIsAuthenticated(true);
      onSuccess();
      void statsService.trackFunnel('auth_done');
    } catch (err) {
      console.error(`[SocialSignIn] ${provider} login failed:`, err);
      onError(err instanceof Error ? err.message : t('auth:auth.loginFailedRetry'));
    } finally {
      setBusy(null);
    }
  };

  if (providers.length === 0) return null;

  const divider = (
    <Divider sx={{ my: 0.5 }}>
      <Typography variant="caption" color="text.secondary">
        {t('common:common.or', 'OR')}
      </Typography>
    </Divider>
  );
  const label = (provider: SocialProvider) =>
    mode === 'bind'
      ? provider === 'apple' ? t('account:deviceBind.appleButton') : t('account:deviceBind.googleButton')
      : provider === 'apple' ? t('auth:auth.continueWithApple') : t('auth:auth.continueWithGoogle');

  return (
    <Stack spacing={1.5} sx={{ '& > *': { flexShrink: 0 } }}>
      {mode === 'login' && divider}
      {providers.map((provider) => (
        <Button
          key={provider}
          fullWidth
          size="large"
          variant={provider === 'apple' && !isDark ? 'contained' : 'outlined'}
          disabled={disabled || busy !== null}
          onClick={() => handleSignIn(provider)}
          startIcon={
            busy === provider ? (
              <CircularProgress size={18} color="inherit" />
            ) : provider === 'apple' ? (
              <AppleIcon />
            ) : (
              <GoogleGlyph />
            )
          }
          sx={
            provider !== 'apple'
              ? undefined
              : isDark
                ? // A solid white button reads as a text field in the dark dialog (same shape as the
                  // email input), so dark mode uses the outlined style Google uses, in white.
                  {
                    color: '#fff',
                    borderColor: 'rgba(255,255,255,0.7)',
                    '&:hover': { borderColor: '#fff', bgcolor: 'rgba(255,255,255,0.08)' },
                  }
                : // Apple HIG: black button on light backgrounds.
                  { bgcolor: '#000', color: '#fff', '&:hover': { bgcolor: '#1a1a1a' } }
          }
        >
          {label(provider)}
        </Button>
      ))}
      {mode === 'bind' && divider}
    </Stack>
  );
}

export default function SocialSignInButtons(props: SocialSignInButtonsProps) {
  // Checked before any hook runs so shells without the capability (old apps,
  // desktop, web) pay nothing — not even the appConfig fetch.
  const bridge = window._platform?.socialSignIn;
  if (!bridge) return null;
  return <SocialSignInInner {...props} bridge={bridge} />;
}

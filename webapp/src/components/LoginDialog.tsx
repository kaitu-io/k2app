import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useLoginDialogStore } from '../stores/login-dialog.store';
import { useAuthStore } from '../stores/auth.store';

export function LoginDialog() {
  const { t } = useTranslation('auth');
  const { isOpen, close } = useLoginDialogStore();
  const { getAuthCode, login, isLoading } = useAuthStore();
  const [email, setEmail] = useState('');
  const [code, setCode] = useState('');
  const [codeSent, setCodeSent] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (!isOpen) return null;

  const handleSendCode = async () => {
    setError(null);
    try {
      await getAuthCode(email);
      setCodeSent(true);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed');
    }
  };

  const handleLogin = async () => {
    setError(null);
    try {
      await login(email, code);
      close();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed');
    }
  };

  return (
    <div className="fixed inset-0 bg-black/60 z-50 flex items-center justify-center">
      <div className="w-[calc(100%-32px)] max-w-md rounded-xl bg-bg-paper">
        {!codeSent && (
          <div className="bg-bg-gradient rounded-t-xl px-6 pt-6 pb-4">
            <h2 className="text-white font-bold text-lg">{t('auth:login_title', 'Login')}</h2>
          </div>
        )}
        <div className="px-6 pt-6 pb-6 space-y-4">
          {error && <div className="text-sm text-error">{error}</div>}
          {!codeSent ? (
            <>
              <input
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder={t('auth:email_placeholder', 'Enter your email')}
                className="w-full rounded-lg border border-card-border bg-bg-paper text-sm text-text-primary px-3 py-2.5 outline-none"
              />
              <button
                onClick={handleSendCode}
                disabled={isLoading}
                className="w-full rounded-lg bg-primary text-white font-bold py-3.5"
              >
                {t('auth:send_code', 'Send Code')}
              </button>
            </>
          ) : (
            <>
              <input
                type="text"
                value={code}
                onChange={(e) => setCode(e.target.value)}
                placeholder={t('auth:code_placeholder', 'Enter code')}
                className="w-full rounded-lg border border-card-border bg-bg-paper text-sm text-text-primary px-3 py-2.5 outline-none"
              />
              <button
                onClick={handleLogin}
                disabled={isLoading}
                className="w-full rounded-lg bg-primary text-white font-bold py-3.5"
              >
                {t('auth:login', 'Login')}
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAuthStore } from '../stores/auth.store';

interface EmailLoginFormProps {
  onSuccess?: () => void;
  onCodeSent?: () => void;
}

export function EmailLoginForm({ onSuccess, onCodeSent }: EmailLoginFormProps) {
  const { t } = useTranslation('auth');
  const { getAuthCode, login, isLoading } = useAuthStore();
  const [email, setEmail] = useState('');
  const [code, setCode] = useState('');
  const [codeSent, setCodeSent] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSendCode = async () => {
    setError(null);
    try {
      await getAuthCode(email);
      setCodeSent(true);
      onCodeSent?.();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed');
    }
  };

  const handleLogin = async () => {
    setError(null);
    try {
      await login(email, code);
      onSuccess?.();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed');
    }
  };

  return (
    <div className="space-y-4">
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
  );
}

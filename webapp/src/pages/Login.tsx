import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useForm } from 'react-hook-form';
import { z } from 'zod';
import { zodResolver } from '@hookform/resolvers/zod';
import { useAuthStore } from '../stores/auth.store';

const emailSchema = z.object({
  email: z.string().email(),
});

const loginSchema = z.object({
  email: z.string().email(),
  code: z.string().min(4).max(8),
});

type EmailForm = z.infer<typeof emailSchema>;
type LoginForm = z.infer<typeof loginSchema>;

export function Login() {
  const { t } = useTranslation('auth');
  const navigate = useNavigate();
  const { getAuthCode, login, isLoading } = useAuthStore();
  const [codeSent, setCodeSent] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const emailForm = useForm<EmailForm>({
    resolver: zodResolver(emailSchema),
  });

  const loginForm = useForm<LoginForm>({
    resolver: zodResolver(loginSchema),
  });

  const handleGetCode = async (data: EmailForm) => {
    setError(null);
    try {
      await getAuthCode(data.email);
      setCodeSent(true);
      loginForm.setValue('email', data.email);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to send code');
    }
  };

  const handleLogin = async (data: LoginForm) => {
    setError(null);
    try {
      await login(data.email, data.code);
      navigate('/');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Login failed');
    }
  };

  return (
    <div className="flex flex-col items-center justify-center h-screen p-6">
      <h1 className="text-2xl font-bold mb-8">{t('title')}</h1>

      {error && (
        <div className="w-full max-w-sm mb-4 p-3 bg-red-50 text-red-700 rounded text-sm">
          {error}
        </div>
      )}

      {!codeSent ? (
        <form onSubmit={emailForm.handleSubmit(handleGetCode)} className="w-full max-w-sm space-y-4">
          <div>
            <label className="block text-sm font-medium mb-1">{t('email')}</label>
            <input
              {...emailForm.register('email')}
              type="email"
              placeholder={t('emailPlaceholder')}
              className="w-full border rounded px-3 py-2"
              autoFocus
            />
            {emailForm.formState.errors.email && (
              <p className="text-red-500 text-xs mt-1">{emailForm.formState.errors.email.message}</p>
            )}
          </div>
          <button
            type="submit"
            disabled={isLoading}
            className="w-full bg-blue-600 text-white py-2 rounded disabled:opacity-50"
          >
            {t('getCode')}
          </button>
        </form>
      ) : (
        <form onSubmit={loginForm.handleSubmit(handleLogin)} className="w-full max-w-sm space-y-4">
          <div>
            <label className="block text-sm font-medium mb-1">{t('email')}</label>
            <input
              {...loginForm.register('email')}
              type="email"
              className="w-full border rounded px-3 py-2 bg-gray-50"
              readOnly
            />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1">{t('code')}</label>
            <input
              {...loginForm.register('code')}
              type="text"
              placeholder={t('codePlaceholder')}
              className="w-full border rounded px-3 py-2"
              autoFocus
            />
            {loginForm.formState.errors.code && (
              <p className="text-red-500 text-xs mt-1">{loginForm.formState.errors.code.message}</p>
            )}
          </div>
          <button
            type="submit"
            disabled={isLoading}
            className="w-full bg-blue-600 text-white py-2 rounded disabled:opacity-50"
          >
            {isLoading ? '...' : t('login')}
          </button>
        </form>
      )}
    </div>
  );
}

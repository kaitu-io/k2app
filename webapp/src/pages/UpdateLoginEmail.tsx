import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { cloudApi } from '../api/cloud';
import { MembershipGuard } from '../components/MembershipGuard';

function UpdateLoginEmailForm() {
  const { t } = useTranslation('settings');
  const [step, setStep] = useState<1 | 2>(1);
  const [email, setEmail] = useState('');
  const [code, setCode] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const [isLoading, setIsLoading] = useState(false);

  const handleSendCode = async () => {
    setError(null);
    setIsLoading(true);
    try {
      await cloudApi.sendEmailCode(email);
      setStep(2);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to send code');
    } finally {
      setIsLoading(false);
    }
  };

  const handleSubmit = async () => {
    setError(null);
    setIsLoading(true);
    try {
      await cloudApi.updateEmail(email, code);
      setSuccess(true);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to update email');
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="p-4 space-y-6">
      <h1 className="text-xl font-semibold">{t('updateEmail.title')}</h1>

      {success && (
        <div className="p-3 rounded-lg bg-success-bg text-success text-sm">
          {t('updateEmail.success')}
        </div>
      )}

      {error && (
        <div className="p-3 rounded-lg bg-error-bg text-error text-sm">
          {error}
        </div>
      )}

      {/* Step indicator */}
      <div className="flex items-center gap-2 text-sm">
        <span className={step === 1 ? 'text-[--color-primary] font-bold' : 'text-text-secondary'}>
          {t('updateEmail.step1')}
        </span>
        <span className="text-text-disabled">/</span>
        <span className={step === 2 ? 'text-[--color-primary] font-bold' : 'text-text-secondary'}>
          {t('updateEmail.step2')}
        </span>
      </div>

      {step === 1 && (
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-1 text-text-secondary">
              {t('updateEmail.newEmail')}
            </label>
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder={t('updateEmail.emailPlaceholder')}
              className="w-full rounded-lg border border-card-border bg-bg-paper text-sm text-text-primary px-3 py-2.5 outline-none"
            />
          </div>
          <button
            onClick={handleSendCode}
            disabled={isLoading || !email}
            className="w-full rounded-lg bg-primary text-white font-bold py-3.5 disabled:opacity-50"
          >
            {t('updateEmail.sendCode')}
          </button>
        </div>
      )}

      {step === 2 && (
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-1 text-text-secondary">
              {t('updateEmail.newEmail')}
            </label>
            <input
              type="email"
              value={email}
              readOnly
              className="w-full rounded-lg border border-card-border bg-bg-paper text-sm text-text-disabled px-3 py-2.5 outline-none"
            />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1 text-text-secondary">
              {t('updateEmail.code')}
            </label>
            <input
              type="text"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder={t('updateEmail.codePlaceholder')}
              className="w-full rounded-lg border border-card-border bg-bg-paper text-sm text-text-primary px-3 py-2.5 outline-none"
            />
          </div>
          <button
            onClick={handleSubmit}
            disabled={isLoading || !code}
            className="w-full rounded-lg bg-primary text-white font-bold py-3.5 disabled:opacity-50"
          >
            {t('updateEmail.submit')}
          </button>
        </div>
      )}
    </div>
  );
}

export function UpdateLoginEmail() {
  return (
    <MembershipGuard>
      <UpdateLoginEmailForm />
    </MembershipGuard>
  );
}

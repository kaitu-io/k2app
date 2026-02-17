import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useLoginDialogStore } from '../stores/login-dialog.store';
import { EmailLoginForm } from './EmailLoginForm';

export function LoginDialog() {
  const { t } = useTranslation('auth');
  const { isOpen, close } = useLoginDialogStore();
  const [showTitle, setShowTitle] = useState(true);

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 bg-black/60 z-50 flex items-center justify-center">
      <div className="w-[calc(100%-32px)] max-w-md rounded-xl bg-bg-paper">
        {showTitle && (
          <div className="bg-bg-gradient rounded-t-xl px-6 pt-6 pb-4">
            <h2 className="text-white font-bold text-lg">{t('title')}</h2>
          </div>
        )}
        <div className="px-6 pt-6 pb-6">
          <EmailLoginForm onSuccess={close} onCodeSent={() => setShowTitle(false)} />
        </div>
      </div>
    </div>
  );
}

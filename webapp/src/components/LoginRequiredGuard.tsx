import { useEffect } from 'react';
import { useAuthStore } from '../stores/auth.store';
import { useLoginDialogStore } from '../stores/login-dialog.store';

export function LoginRequiredGuard({ children }: { children: React.ReactNode }) {
  const { isLoggedIn } = useAuthStore();
  const { open } = useLoginDialogStore();

  useEffect(() => {
    if (!isLoggedIn) {
      open('guard');
    }
  }, [isLoggedIn, open]);

  if (!isLoggedIn) return null;
  return <>{children}</>;
}

import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useUserStore } from '../stores/user.store';

export function MembershipGuard({ children }: { children: React.ReactNode }) {
  const navigate = useNavigate();
  const { getMembershipStatus } = useUserStore();
  const status = getMembershipStatus();

  useEffect(() => {
    if (!status) {
      navigate('/purchase', { replace: true });
    }
  }, [status, navigate]);

  if (!status) return null;
  return <>{children}</>;
}

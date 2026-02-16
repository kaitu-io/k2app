import { useUserStore } from '../stores/user.store';

export function useUser() {
  const { user, isLoading, error } = useUserStore();
  return { user, isLoading, error };
}

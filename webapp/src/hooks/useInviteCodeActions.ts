import { useInviteStore } from '../stores/invite.store';

export function useInviteCodeActions() {
  const {
    latestCode,
    codes,
    isLoading,
    error,
    loadLatest,
    generateCode,
    updateRemark,
    loadAllCodes,
  } = useInviteStore();

  return {
    latestCode,
    codes,
    isLoading,
    error,
    loadLatest,
    generateCode,
    updateRemark,
    loadAllCodes,
  };
}

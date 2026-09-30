'use client';

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { api, type UserProfile } from '@/lib/api';
import { appEvents } from '@/lib/events';

interface AuthContextValue {
  /** null = signed out (or not yet known — check `loading`). */
  profile: UserProfile | null;
  loading: boolean;
  /** Re-read the profile (after login, after checkout). Returns the fresh value. */
  refresh: () => Promise<UserProfile | null>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [profile, setProfile] = useState<UserProfile | null>(null);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    try {
      const p = await api.getUserProfile({ redirectOn401: false });
      setProfile(p);
      return p;
    } catch {
      setProfile(null);
      return null;
    } finally {
      setLoading(false);
    }
  }, []);

  const logout = useCallback(async () => {
    await api.logout();
    setProfile(null);
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    const clear = () => setProfile(null);
    appEvents.on('auth:unauthorized', clear);
    return () => appEvents.off('auth:unauthorized', clear);
  }, []);

  const value = useMemo(() => ({ profile, loading, refresh, logout }), [profile, loading, refresh, logout]);
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}

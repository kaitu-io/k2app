"use client";

import React, {
  createContext,
  useContext,
  useState,
  ReactNode,
  useCallback,
  useEffect,
} from "react";
import { appEvents } from "@/lib/events";
import { redirectToLogin } from "@/lib/auth";
import { api } from "@/lib/api";
import { safeStorage } from "@/lib/safeStorage";

interface User {
  id: number;
  email: string;
  isAdmin: boolean;
  roles: number;
  /**
   * Whether the user has a password on file. Populated by `getCurrentUser`
   * on mount and also by both login paths (OTP + password — server now
   * returns this field on the WebLogin response, so the CTA on
   * `/account/security` resolves correctly on the first render after login).
   * Consumers should still default to `false` when undefined, as a
   * defensive measure against future shape regressions.
   */
  hasPassword?: boolean;
}

interface AuthContextType {
  user: User | null;
  /**
   * Apply login credentials. `accessToken` enables a localStorage Bearer
   * fallback when the browser fails to persist the HttpOnly cookie from
   * the same response (iOS WeChat WKWebView). Cookie path stays preferred.
   */
  login: (user: User, accessToken?: string) => Promise<void>;
  logout: () => void;
  navigateToLogin: () => void;
  isAuthenticated: boolean;
  isAuthLoading: boolean;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export const AuthProvider = ({ children }: { children: ReactNode }) => {
  const [user, setUser] = useState<User | null>(null);
  const [isAuthLoading, setIsAuthLoading] = useState(true);

  const navigateToLogin = useCallback(() => {
    redirectToLogin();
  }, []);

  const logout = useCallback(async (nextUrl?: string) => {
    // Call server to clear HttpOnly cookies
    await api.logout();
    // Clear local state
    setUser(null);
    // Navigate to login
    redirectToLogin(nextUrl);
  }, []);

  const clearAuthState = useCallback(() => {
    // Only clear React state (used by auth:unauthorized event)
    setUser(null);
  }, []);

  const login = useCallback(async (newUser: User, accessToken?: string) => {
    // Verify cookie persisted (or stash Bearer fallback) BEFORE setUser, so
    // downstream effects that fire on isAuthenticated=true (e.g. PurchaseClient
    // fetching /api/delegate) find a usable auth credential in the first
    // request they make.
    if (accessToken) {
      await api.applyLoginCredentials(accessToken);
    }
    setUser(newUser);
  }, []);

  // Check auth status on mount via API (cookie sent automatically)
  useEffect(() => {
    const checkAuthStatus = async () => {
      try {
        // Special case: embed mode with Bearer token
        const embedToken = safeStorage.get("embed_auth_token");
        if (embedToken) {
          console.log("[AuthContext] Embed mode detected");
          // For embed mode, we still need to verify with API
          // The embed token is passed via Authorization header in api.ts
          try {
            const userInfo = await api.getCurrentUser({ autoRedirectToAuth: false });
            setUser({
              id: userInfo.id,
              email: userInfo.email || "",
              isAdmin: userInfo.isAdmin || false,
              roles: userInfo.roles ?? 1,
              hasPassword: userInfo.hasPassword,
            });
          } catch {
            console.log("[AuthContext] Embed token invalid");
            safeStorage.remove("embed_auth_token");
          }
          setIsAuthLoading(false);
          return;
        }

        // Normal flow: check auth via HttpOnly cookie
        const userInfo = await api.getCurrentUser({ autoRedirectToAuth: false });
        setUser({
          id: userInfo.id,
          email: userInfo.email || "",
          isAdmin: userInfo.isAdmin || false,
          roles: userInfo.roles ?? 1,
          hasPassword: userInfo.hasPassword,
        });
      } catch {
        // Not authenticated or error - user stays null
        setUser(null);
      } finally {
        setIsAuthLoading(false);
      }
    };

    checkAuthStatus();
  }, []);

  // Handle auth:unauthorized event
  useEffect(() => {
    const handleUnauthorized = () => {
      console.log("[AuthContext] Unauthorized event - clearing state");
      clearAuthState();
    };

    appEvents.on("auth:unauthorized", handleUnauthorized);
    return () => {
      appEvents.off("auth:unauthorized", handleUnauthorized);
    };
  }, [clearAuthState]);

  return (
    <AuthContext.Provider
      value={{
        user,
        login,
        logout,
        navigateToLogin,
        isAuthenticated: !!user,
        isAuthLoading,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
};

export const useAuth = () => {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return context;
};

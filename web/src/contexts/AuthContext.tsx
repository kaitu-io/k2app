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

interface User {
  id: number;
  email: string;
  isAdmin: boolean;
}

interface AuthContextType {
  user: User | null;
  login: (user: User) => void;
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

  const login = useCallback((newUser: User) => {
    // Server already set HttpOnly cookie, just update React state
    setUser(newUser);
  }, []);

  // Check auth status on mount via API (cookie sent automatically)
  useEffect(() => {
    const checkAuthStatus = async () => {
      try {
        // Special case: embed mode with Bearer token
        const embedToken = localStorage.getItem("embed_auth_token");
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
            });
          } catch {
            console.log("[AuthContext] Embed token invalid");
            localStorage.removeItem("embed_auth_token");
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

  if (isAuthLoading) {
    return (
      <div className="flex items-center justify-center h-screen">
        <div className="animate-spin rounded-full h-16 w-16 border-t-2 border-b-2 border-primary"></div>
      </div>
    );
  }

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

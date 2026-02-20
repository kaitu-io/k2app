"use client";

import React, {
  createContext,
  useContext,
  useState,
  ReactNode,
  useEffect,
} from "react";
import { api, type AppConfig } from "@/lib/api";

const CACHE_KEY = "app_config_cache";
const CACHE_TTL = 60 * 60 * 1000; // 1 hour in milliseconds

interface CachedAppConfig {
  data: AppConfig;
  timestamp: number;
}

function getCachedConfig(): AppConfig | null {
  if (typeof window === "undefined") return null;

  try {
    const cached = localStorage.getItem(CACHE_KEY);
    if (!cached) return null;

    const { data, timestamp }: CachedAppConfig = JSON.parse(cached);
    const now = Date.now();

    // Check if cache is still valid
    if (now - timestamp < CACHE_TTL) {
      return data;
    }

    // Cache expired, remove it
    localStorage.removeItem(CACHE_KEY);
    return null;
  } catch {
    localStorage.removeItem(CACHE_KEY);
    return null;
  }
}

function setCachedConfig(config: AppConfig): void {
  if (typeof window === "undefined") return;

  try {
    const cached: CachedAppConfig = {
      data: config,
      timestamp: Date.now(),
    };
    localStorage.setItem(CACHE_KEY, JSON.stringify(cached));
  } catch (error) {
    console.error("Failed to cache app config:", error);
  }
}

interface AppConfigContextType {
  appConfig: AppConfig | null;
  isLoading: boolean;
}

const AppConfigContext = createContext<AppConfigContextType | undefined>(undefined);

export const AppConfigProvider = ({ children }: { children: ReactNode }) => {
  const [appConfig, setAppConfig] = useState<AppConfig | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    const loadAppConfig = async () => {
      // Try to get from cache first
      const cached = getCachedConfig();
      if (cached) {
        setAppConfig(cached);
        setIsLoading(false);
        return;
      }

      // Fetch from API
      try {
        const config = await api.getAppConfig({ autoRedirectToAuth: false });
        setAppConfig(config);
        setCachedConfig(config);
      } catch (error) {
        console.error("Failed to fetch app config:", error);
      } finally {
        setIsLoading(false);
      }
    };
    loadAppConfig();
  }, []);

  return (
    <AppConfigContext.Provider value={{ appConfig, isLoading }}>
      {children}
    </AppConfigContext.Provider>
  );
};

export const useAppConfig = () => {
  const context = useContext(AppConfigContext);
  if (context === undefined) {
    throw new Error("useAppConfig must be used within an AppConfigProvider");
  }
  return context;
};

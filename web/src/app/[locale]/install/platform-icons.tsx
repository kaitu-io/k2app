import React from 'react';

export const PLATFORM_IDS = ['windows', 'macos', 'linux', 'ios', 'android'] as const;
export type PlatformId = (typeof PLATFORM_IDS)[number];

export const platformIcons: Record<string, React.FC<{ className?: string }>> = {
  windows: ({ className }) => (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48" fill="currentColor" className={className}>
      <path d="M2 6.5L20.3 3.8V22.5H2V6.5ZM22.5 3.5L46 0V22.5H22.5V3.5ZM2 24.5H20.3V43.2L2 40.5V24.5ZM22.5 24.5H46V47L22.5 43.5V24.5Z"/>
    </svg>
  ),
  macos: ({ className }) => (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" className={className}>
      <path d="M18.71 19.5c-.83 1.24-1.71 2.45-3.05 2.47-1.34.03-1.77-.79-3.29-.79-1.53 0-2 .77-3.27.82-1.31.05-2.3-1.32-3.14-2.53C4.25 17 2.94 12.45 4.7 9.39c.87-1.52 2.43-2.48 4.12-2.51 1.28-.02 2.5.87 3.29.87.78 0 2.26-1.07 3.8-.91.65.03 2.47.26 3.64 1.98-.09.06-2.17 1.28-2.15 3.81.03 3.02 2.65 4.03 2.68 4.04-.03.07-.42 1.44-1.38 2.83M13 3.5c.73-.83 1.94-1.46 2.94-1.5.13 1.17-.34 2.35-1.04 3.19-.69.85-1.83 1.51-2.95 1.42-.15-1.15.41-2.35 1.05-3.11z"/>
    </svg>
  ),
  linux: ({ className }) => (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100" className={className}>
      {/* Tux penguin — body */}
      <ellipse cx="50" cy="58" rx="28" ry="34" fill="currentColor"/>
      {/* Head */}
      <circle cx="50" cy="22" r="18" fill="currentColor"/>
      {/* White belly */}
      <ellipse cx="50" cy="62" rx="18" ry="26" fill="white" opacity="0.9"/>
      {/* White face */}
      <ellipse cx="50" cy="26" rx="12" ry="10" fill="white" opacity="0.9"/>
      {/* Eyes */}
      <circle cx="44" cy="22" r="3" fill="currentColor"/>
      <circle cx="56" cy="22" r="3" fill="currentColor"/>
      <circle cx="44.8" cy="21.2" r="1" fill="white"/>
      <circle cx="56.8" cy="21.2" r="1" fill="white"/>
      {/* Beak */}
      <path d="M46 28 L50 34 L54 28 Z" fill="#f59e0b"/>
      {/* Left flipper */}
      <ellipse cx="22" cy="54" rx="8" ry="20" fill="currentColor" transform="rotate(15 22 54)"/>
      {/* Right flipper */}
      <ellipse cx="78" cy="54" rx="8" ry="20" fill="currentColor" transform="rotate(-15 78 54)"/>
      {/* Left foot */}
      <ellipse cx="38" cy="92" rx="10" ry="5" fill="#f59e0b"/>
      {/* Right foot */}
      <ellipse cx="62" cy="92" rx="10" ry="5" fill="#f59e0b"/>
    </svg>
  ),
  ios: ({ className }) => (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" className={className}>
      <path d="M15.5 1h-8C6.12 1 5 2.12 5 3.5v17C5 21.88 6.12 23 7.5 23h8c1.38 0 2.5-1.12 2.5-2.5v-17C18 2.12 16.88 1 15.5 1zm-4 21c-.83 0-1.5-.67-1.5-1.5s.67-1.5 1.5-1.5 1.5.67 1.5 1.5-.67 1.5-1.5 1.5zm4.5-4H7V4h9v14z"/>
    </svg>
  ),
  android: ({ className }) => (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48" fill="currentColor" className={className}>
      <path d="M15.4 8.8l-2.9-5c-.2-.4-.1-.8.3-1 .4-.2.8-.1 1 .3l2.9 5.1c2.2-1 4.7-1.5 7.3-1.5s5.1.6 7.3 1.5l2.9-5.1c.2-.4.6-.5 1-.3.4.2.5.6.3 1l-2.9 5c5.1 2.5 8.5 7.3 8.5 12.8H6.9c0-5.5 3.4-10.3 8.5-12.8zM18 16.5c-.8 0-1.5.7-1.5 1.5s.7 1.5 1.5 1.5 1.5-.7 1.5-1.5-.7-1.5-1.5-1.5zm12 0c-.8 0-1.5.7-1.5 1.5s.7 1.5 1.5 1.5 1.5-.7 1.5-1.5-.7-1.5-1.5-1.5zM6.9 24h34.2v16c0 1.7-1.3 3-3 3H9.9c-1.7 0-3-1.3-3-3V24z"/>
    </svg>
  ),
};

export function PlatformIcon({ type, className }: { type: string; className?: string }) {
  const Icon = platformIcons[type] || platformIcons.windows;
  return <Icon className={className} />;
}

export const PLATFORM_COLORS: Record<string, string> = {
  windows: 'text-blue-400',
  macos: 'text-gray-300',
  linux: 'text-amber-400',
  ios: 'text-blue-400',
  android: 'text-green-400',
};

export const PLATFORM_LABELS: Record<PlatformId, string> = {
  windows: 'Windows',
  macos: 'macOS',
  linux: 'Linux',
  ios: 'iOS',
  android: 'Android',
};

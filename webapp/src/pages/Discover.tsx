import { useCallback, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAuthStore } from '../stores/auth.store';

const DISCOVER_URL = 'https://kaitu.io/discover';

export function Discover() {
  const { t } = useTranslation('settings');
  const { token } = useAuthStore();
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [isLoading, setIsLoading] = useState(true);

  const handleIframeLoad = useCallback(() => {
    setIsLoading(false);

    // Broadcast auth token to the embedded page via postMessage
    if (iframeRef.current?.contentWindow && token) {
      iframeRef.current.contentWindow.postMessage(
        { type: 'auth', token },
        '*'
      );
    }
  }, [token]);

  return (
    <div className="relative h-full">
      {/* Progress bar at top during load */}
      {isLoading && (
        <div className="absolute top-0 left-0 right-0 z-10">
          <div className="h-1 bg-primary animate-pulse rounded-full" />
        </div>
      )}

      {/* Loading state */}
      {isLoading && (
        <div className="flex items-center justify-center py-8 bg-[--color-bg-paper]">
          <span className="text-sm text-text-secondary">
            {t('discover.loading')}
          </span>
        </div>
      )}

      <iframe
        ref={iframeRef}
        data-testid="discover-iframe"
        src={DISCOVER_URL}
        title={t('discover.title')}
        className="w-full border-none"
        style={{ height: 'calc(100vh - 120px)' }}
        onLoad={handleIframeLoad}
      />
    </div>
  );
}

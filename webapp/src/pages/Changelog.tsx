import { Box, LinearProgress } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useState, useRef, useEffect } from 'react';
import { useKaituBridge } from '../hooks/useKaituBridge';
import { useAuth } from "../stores";
import { useAppLinks } from '../hooks/useAppLinks';

export default function Changelog() {
  const { t } = useTranslation();
  const { isAuthenticated } = useAuth();
  const { links, currentLang } = useAppLinks();
  const [isLoading, setIsLoading] = useState(true);
  const [progress, setProgress] = useState(0);
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const progressIntervalRef = useRef<NodeJS.Timeout | null>(null);

  // Setup WebView Bridge integration
  const {
    injectBridge,
    broadcastAuthStateChange
  } = useKaituBridge({
    onShowAlert: (title, message) => alert(`${title}\n\n${message}`),
  });

  // Build changelog URL with embed flag and locale
  const changelogUrl = `${links.changelogUrl}?embed=true`;
  const iframeUrl = changelogUrl.replace('kaitu.io/', `kaitu.io/${currentLang}/`);

  // Start progress bar animation
  const startProgress = () => {
    setIsLoading(true);
    setProgress(0);

    if (progressIntervalRef.current) {
      clearInterval(progressIntervalRef.current);
    }

    progressIntervalRef.current = setInterval(() => {
      setProgress(prev => {
        if (prev >= 90) {
          return prev; // Stop at 90%, wait for iframe to finish loading
        }
        return prev + Math.random() * 15; // Random increment to simulate real loading
      });
    }, 200);
  };

  // Complete progress bar
  const completeProgress = () => {
    if (progressIntervalRef.current) {
      clearInterval(progressIntervalRef.current);
    }

    setProgress(100);

    // Delay hiding progress bar to let user see completion
    setTimeout(() => {
      setIsLoading(false);
      setProgress(0);
    }, 500);
  };

  // Handle iframe load event
  const handleIframeLoad = () => {
    completeProgress();

    // Inject Kaitu WebView Bridge
    if (iframeRef.current) {
      injectBridge(iframeRef.current);
    }
  };

  // Disable iframe right-click menu
  const handleContextMenu = (e: React.MouseEvent) => {
    e.preventDefault();
    return false;
  };

  // Restart progress when URL changes
  useEffect(() => {
    startProgress();
  }, [iframeUrl]);

  // Listen for iframe messages to handle external links
  useEffect(() => {
    const handleMessage = (event: MessageEvent) => {
      // Ensure message is from our iframe
      if (event.origin !== 'https://www.kaitu.io') return;

      // Check for link click events
      if (event.data?.type === 'external-link' && event.data?.url) {
        // Open link in default browser using bridge
        window._platform!.openExternal?.(event.data.url).catch(console.error);
      }
    };

    window.addEventListener('message', handleMessage);

    return () => {
      window.removeEventListener('message', handleMessage);
    };
  }, []);

  // Listen for auth state changes and broadcast to iframe
  useEffect(() => {
    broadcastAuthStateChange(isAuthenticated);
  }, [isAuthenticated, broadcastAuthStateChange]);

  // Cleanup timers
  useEffect(() => {
    return () => {
      if (progressIntervalRef.current) {
        clearInterval(progressIntervalRef.current);
      }
    };
  }, []);

  return (
    <Box sx={{
      width: '100%',
      height: '100%',
      display: 'flex',
      flexDirection: 'column',
      position: 'relative'
    }}>
      {/* Progress bar */}
      {isLoading && (
        <Box sx={{
          position: 'absolute',
          top: 0,
          left: 0,
          right: 0,
          zIndex: 1000,
          backgroundColor: 'rgba(255, 255, 255, 0.9)',
          borderRadius: '8px 8px 0 0'
        }}>
          <LinearProgress
            variant="determinate"
            value={progress}
            sx={{
              height: 4,
              borderRadius: '8px 8px 0 0',
              '& .MuiLinearProgress-bar': {
                borderRadius: '8px 8px 0 0',
                transition: 'transform 0.4s ease-in-out'
              }
            }}
          />
        </Box>
      )}

      <iframe
        ref={iframeRef}
        src={iframeUrl}
        onLoad={handleIframeLoad}
        onContextMenu={handleContextMenu}
        style={{
          width: '100%',
          height: '100%',
          border: 'none',
          borderRadius: '8px',
          opacity: isLoading ? 0.7 : 1,
          transition: 'opacity 0.3s ease-in-out'
        }}
        title={t('changelog.title', 'Changelog')}
        loading="lazy"
      />
    </Box>
  );
}

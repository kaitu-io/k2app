import { Box, LinearProgress } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useState, useRef, useEffect, useLayoutEffect, useCallback } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate, useLocation } from 'react-router-dom';
import { useKaituBridge } from '../hooks/useKaituBridge';
import { useAuth } from "../stores";
import { useAppLinks } from '../hooks/useAppLinks';
import { allowedEmbedOrigins, isSafeExternalUrl } from '../utils/embed-origins';

interface OverlayRect { top: number; left: number; width: number; height: number; }

export default function Discover() {
  const { t } = useTranslation();
  const { isAuthenticated } = useAuth();
  const { links } = useAppLinks();
  const navigate = useNavigate();
  const location = useLocation();
  const [isLoading, setIsLoading] = useState(true);
  const [progress, setProgress] = useState(0);
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const progressIntervalRef = useRef<NodeJS.Timeout | null>(null);

  // The iframe is rendered into the unscaled #discover-overlay layer (a sibling
  // of #root) so it escapes the viewport-scaling `zoom` applied to #root — under
  // which macOS/iOS WebKit mis-renders the cross-origin iframe (white strip).
  // A placeholder div stays in the normal (scaled) layout to reserve space and
  // define where the overlay iframe must be positioned. See main.tsx / index.html.
  const placeholderRef = useRef<HTMLDivElement>(null);
  const [overlayRect, setOverlayRect] = useState<OverlayRect | null>(null);
  const isActive = location.pathname === '/discover';

  // 设置WebView Bridge集成
  const {
    injectBridge,
    broadcastAuthStateChange
  } = useKaituBridge({
    onShowAlert: (title, message) => alert(`${title}\n\n${message}`),
  });

  // 使用统一的链接管理获取 Discovery URL
  const iframeUrl = links.discoveryUrl;

  // Keep the overlay iframe aligned with the placeholder's on-screen rect.
  // getBoundingClientRect() already returns real (post-#root-zoom) pixels, which
  // is exactly the coordinate space of the unscaled overlay's position:fixed.
  const measureOverlay = useCallback(() => {
    const el = placeholderRef.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    setOverlayRect((prev) =>
      prev && prev.top === r.top && prev.left === r.left &&
      prev.width === r.width && prev.height === r.height
        ? prev
        : { top: r.top, left: r.left, width: r.width, height: r.height }
    );
  }, []);

  useLayoutEffect(() => {
    if (!isActive) return;
    const el = placeholderRef.current;
    if (!el) return;
    measureOverlay();
    const ro = new ResizeObserver(measureOverlay);
    ro.observe(el);
    window.addEventListener('resize', measureOverlay);
    // Re-measure after layout / tab-switch / banner animations settle.
    const raf = requestAnimationFrame(measureOverlay);
    const settle = setTimeout(measureOverlay, 250);
    return () => {
      ro.disconnect();
      window.removeEventListener('resize', measureOverlay);
      cancelAnimationFrame(raf);
      clearTimeout(settle);
    };
  }, [isActive, measureOverlay]);

  // 启动进度条动画
  const startProgress = () => {
    setIsLoading(true);
    setProgress(0);

    if (progressIntervalRef.current) {
      clearInterval(progressIntervalRef.current);
    }

    progressIntervalRef.current = setInterval(() => {
      setProgress(prev => {
        if (prev >= 90) {
          return prev; // 在90%处停止，等待iframe真正加载完成
        }
        return prev + Math.random() * 15; // 随机增长，模拟真实加载
      });
    }, 200);
  };

  // 完成进度条
  const completeProgress = () => {
    if (progressIntervalRef.current) {
      clearInterval(progressIntervalRef.current);
    }

    setProgress(100);

    // 延迟隐藏进度条，让用户看到完成状态
    setTimeout(() => {
      setIsLoading(false);
      setProgress(0);
    }, 500);
  };

  // iframe加载事件处理
  const handleIframeLoad = () => {
    completeProgress();

    // 注入Kaitu WebView Bridge
    if (iframeRef.current) {
      injectBridge(iframeRef.current);
    }
  };

  // 禁用iframe右键菜单
  const handleContextMenu = (e: React.MouseEvent) => {
    e.preventDefault();
    return false;
  };

  // 当URL变化时重新开始进度
  useEffect(() => {
    startProgress();
  }, [iframeUrl]);

  // 监听iframe消息处理外部链接
  useEffect(() => {
    const embedOrigins = allowedEmbedOrigins(iframeUrl);
    const handleMessage = (event: MessageEvent) => {
      // 确保消息来源是我们的 iframe：origin 从 iframeUrl 派生（裸域 + www 子域），
      // 因此既跟随 appConfig 下发的 baseURL，又天然品牌中立。
      if (!embedOrigins.has(event.origin)) return;

      // 检查是否是链接点击事件
      if (event.data?.type === 'external-link' && isSafeExternalUrl(event.data?.url)) {
        // 使用bridge在默认浏览器中打开链接
        window._platform!.openExternal?.(event.data.url).catch(console.error);
      }

      // 内部路由导航 (e.g. /android-install with app params)
      if (event.data?.type === 'bridge_navigate' && event.data?.path) {
        const { path, params } = event.data;
        const search = params ? `?${new URLSearchParams(params)}` : '';
        navigate(`${path}${search}`);
      }
    };

    window.addEventListener('message', handleMessage);

    return () => {
      window.removeEventListener('message', handleMessage);
    };
  }, [iframeUrl, navigate]);

  // 监听认证状态变化并广播到iframe
  useEffect(() => {
    broadcastAuthStateChange(isAuthenticated);
  }, [isAuthenticated, broadcastAuthStateChange]);

  // 清理定时器
  useEffect(() => {
    return () => {
      if (progressIntervalRef.current) {
        clearInterval(progressIntervalRef.current);
      }
    };
  }, []);

  // Overlay content (iframe + progress bar), rendered into the unscaled layer.
  const overlayNode =
    typeof document !== 'undefined' ? document.getElementById('discover-overlay') : null;

  const visible = isActive && !!overlayRect;
  const rect: OverlayRect = overlayRect ?? { top: 0, left: 0, width: 0, height: 0 };

  const overlayContent = (
    <>
      <iframe
        ref={iframeRef}
        src={iframeUrl}
        // No allow-popups: window.open/target=_blank inside the iframe is
        // engine-blocked, so nothing can open in the webview even before the
        // site's embed-interceptor script attaches. Links open externally via
        // the 'external-link' postMessage path (unaffected by sandbox).
        sandbox="allow-scripts allow-same-origin allow-forms"
        onLoad={handleIframeLoad}
        onContextMenu={handleContextMenu}
        style={{
          position: 'fixed',
          top: rect.top,
          left: rect.left,
          width: rect.width,
          height: rect.height,
          border: 'none',
          // Dark backstop so any momentary gap is dark, never the system white.
          background: '#0f0f13',
          opacity: isLoading ? 0.7 : 1,
          transition: 'opacity 0.3s ease-in-out',
          display: visible ? 'block' : 'none',
          zIndex: 1,
        }}
        title={t('nav:navigation.discover', 'Discover')}
        loading="lazy"
      />
      {/* 进度条 — rendered above the iframe within the unscaled overlay */}
      {isLoading && visible && (
        <Box sx={{
          position: 'fixed',
          top: rect.top,
          left: rect.left,
          width: rect.width,
          zIndex: 2,
          backgroundColor: 'rgba(255, 255, 255, 0.9)',
        }}>
          <LinearProgress
            variant="determinate"
            value={progress}
            sx={{
              height: 4,
              '& .MuiLinearProgress-bar': {
                transition: 'transform 0.4s ease-in-out',
              },
            }}
          />
        </Box>
      )}
    </>
  );

  // The placeholder fills the tab's content area (scaled). Its rect drives the
  // overlay iframe's position. The iframe itself lives in #discover-overlay.
  return (
    <Box
      ref={placeholderRef}
      sx={{
        width: '100%',
        height: '100%',
        position: 'relative',
      }}
    >
      {overlayNode ? createPortal(overlayContent, overlayNode) : overlayContent}
    </Box>
  );
}

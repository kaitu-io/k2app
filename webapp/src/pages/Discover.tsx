import { Box, LinearProgress } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useState, useRef, useEffect } from 'react';
import { useKaituBridge } from '../hooks/useKaituBridge';
import { useAuth } from "../stores";
import { useAppLinks } from '../hooks/useAppLinks';

export default function Discover() {
  const { t } = useTranslation();
  const { isAuthenticated } = useAuth();
  const { links } = useAppLinks();
  const [isLoading, setIsLoading] = useState(true);
  const [progress, setProgress] = useState(0);
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const progressIntervalRef = useRef<NodeJS.Timeout | null>(null);

  // 设置WebView Bridge集成
  const {
    injectBridge,
    broadcastAuthStateChange
  } = useKaituBridge({
    onShowAlert: (title, message) => alert(`${title}\n\n${message}`),
  });

  // 使用统一的链接管理获取 Discovery URL
  const iframeUrl = links.discoveryUrl;

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
    const handleMessage = (event: MessageEvent) => {
      // 确保消息来源是我们的iframe
      if (event.origin !== 'https://www.kaitu.io') return;
      
      // 检查是否是链接点击事件
      if (event.data?.type === 'external-link' && event.data?.url) {
        // 使用bridge在默认浏览器中打开链接
        window._platform!.openExternal?.(event.data.url).catch(console.error);
      }
    };

    window.addEventListener('message', handleMessage);
    
    return () => {
      window.removeEventListener('message', handleMessage);
    };
  }, []);

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

  return (
    <Box sx={{
      width: '100%',
      height: '100%', // Layout already handles bottom navigation space
      display: 'flex',
      flexDirection: 'column',
      position: 'relative'
    }}>
      {/* 进度条 */}
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
        title={t('nav:navigation.discover', 'Discover')}
        loading="lazy"
      />
    </Box>
  );
} 
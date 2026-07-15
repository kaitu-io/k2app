/**
 * AddRouterCard — 添加路由器卡片（纯软件 BYO onboarding, Plan 5b）
 *
 * 点「生成路由器连接地址」按钮 mint 出 k2subs URL 给用户复制到路由器面板；
 * 同时 mount 时调用 discoverRouter() 探测同公网下的路由器候选，
 * 若有则展示「打开 http://lanIP:port」直达链接。
 */

import { useEffect, useState } from 'react';
import { Card, CardContent, Typography, Button, Box, Link, Stack } from '@mui/material';
import { useTranslation } from 'react-i18next';
import {
  mintGatewayCredential,
  discoverRouter,
  type RouterCandidate,
} from '../services/private-node-service';

export function AddRouterCard() {
  const { t } = useTranslation();
  const [url, setUrl] = useState('');
  const [candidates, setCandidates] = useState<RouterCandidate[]>([]);

  useEffect(() => {
    discoverRouter()
      .then(setCandidates)
      .catch(() => setCandidates([]));
  }, []);

  const handleMint = async () => {
    try {
      const u = await mintGatewayCredential();
      setUrl(u);
    } catch {
      // mint 失败时不展示地址；用户可重试
    }
  };

  // 路由器管理页必须在系统浏览器打开——裸 target=_blank 会落入 webview 默认行为
  const openRouterAdmin = async (adminUrl: string) => {
    try {
      await window._platform!.openExternal(adminUrl);
    } catch {
      window.open(adminUrl, '_blank', 'noopener,noreferrer');
    }
  };

  return (
    <Card sx={{ mt: 2 }}>
      <CardContent>
        <Typography variant="h6" sx={{ fontSize: '1rem', fontWeight: 700 }}>
          {t('privateNode:privateNode.router.title')}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
          {t('privateNode:privateNode.router.intro')}
        </Typography>

        {candidates.length > 0 && (
          <Stack sx={{ mt: 2 }} spacing={1}>
            <Typography variant="subtitle2">
              {t('privateNode:privateNode.router.detected')}
            </Typography>
            {candidates.map((c, i) => (
              <Link
                key={`${c.lanIP}:${c.port}`}
                data-testid={`add-router-open-${i}`}
                href={`http://${c.lanIP}:${c.port}`}
                onClick={(e) => {
                  e.preventDefault();
                  void openRouterAdmin(`http://${c.lanIP}:${c.port}`);
                }}
              >
                {t('privateNode:privateNode.router.open', { addr: `${c.lanIP}:${c.port}` })}
              </Link>
            ))}
          </Stack>
        )}

        <Button
          data-testid="add-router-mint"
          variant="contained"
          sx={{ mt: 2 }}
          onClick={handleMint}
        >
          {t('privateNode:privateNode.router.mint')}
        </Button>

        {url && (
          <Box sx={{ mt: 2 }}>
            <Typography variant="body2">
              {t('privateNode:privateNode.router.paste')}
            </Typography>
            <Box
              data-testid="add-router-url"
              sx={{
                mt: 1,
                p: 1,
                bgcolor: 'action.hover',
                borderRadius: 1,
                wordBreak: 'break-all',
                fontFamily: 'monospace',
              }}
            >
              {url}
            </Box>
            <Button
              sx={{ mt: 1 }}
              size="small"
              onClick={() => navigator.clipboard?.writeText(url)}
            >
              {t('privateNode:privateNode.router.copy')}
            </Button>
          </Box>
        )}
      </CardContent>
    </Card>
  );
}

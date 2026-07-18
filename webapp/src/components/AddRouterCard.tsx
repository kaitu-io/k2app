/**
 * AddRouterCard — 添加路由器卡片（纯软件 BYO onboarding, Plan 5b）
 *
 * mount 时调用 discoverRouter() 探测同局域网下的路由器候选（GET /api/pair/discover）；
 * 命中候选后展示「前往路由器管理」按钮 → navigate('/router')，由 Router tab 自行做
 * 锚点探测(router.store.runDiscovery)完成后续配对/首配。老固件（无锚点 DNAT 规则，
 * router.store.phase 恒为 'none'）额外展示升级提示。
 *
 * mint 凭证 + 粘贴 URL 首配流程已退役（Router tab 内的 RouterSetupCard 接管，走
 * 锚点直连一键首配），本卡片不再处理。
 */

import { useEffect, useState } from 'react';
import { Card, CardContent, Typography, Button, Stack } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { discoverRouter, type RouterCandidate } from '../services/private-node-service';
import { useRouterStore } from '../stores/router.store';

export function AddRouterCard() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [candidates, setCandidates] = useState<RouterCandidate[]>([]);
  const phase = useRouterStore((s) => s.phase);

  useEffect(() => {
    discoverRouter()
      .then(setCandidates)
      .catch(() => setCandidates([]));
  }, []);

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
          <Stack sx={{ mt: 2 }} spacing={1} alignItems="flex-start">
            <Typography variant="subtitle2">
              {t('privateNode:privateNode.router.detected')}
            </Typography>
            <Button
              data-testid="add-router-manage"
              variant="contained"
              onClick={() => navigate('/router')}
            >
              {t('privateNode:privateNode.router.manage')}
            </Button>
            {phase === 'none' && (
              <Typography
                variant="caption"
                color="text.secondary"
                data-testid="add-router-legacy-hint"
              >
                {t('router:legacy.upgradeHint')}
              </Typography>
            )}
          </Stack>
        )}
      </CardContent>
    </Card>
  );
}

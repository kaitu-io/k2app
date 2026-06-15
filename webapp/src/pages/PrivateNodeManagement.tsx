/**
 * PrivateNodeManagement — 专属节点管理页
 *
 * 列出当前用户的专属节点订阅（usePrivateNodes，SWR 缓存）。
 * 加载中显示 spinner；无节点显示空态；否则每个订阅渲染一张 PrivateNodePanel。
 *
 * 路由由 App.tsx 注册，并用 LoginRequiredGuard 包裹（pagePath="/private-node"），
 * 因此本页面无需自行处理未登录态。
 */

import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { Box, Typography, CircularProgress, Button } from '@mui/material';
import { usePrivateNodes } from '../hooks/usePrivateNodes';
import PrivateNodePanel from '../components/PrivateNodePanel';
import { AddRouterCard } from '../components/AddRouterCard';

export default function PrivateNodeManagement() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { nodes, loading } = usePrivateNodes();

  return (
    <Box sx={{ p: 2, maxWidth: 720, mx: 'auto' }}>
      <Typography variant="h6" fontWeight={700} sx={{ mb: 2, fontSize: '1.05rem' }}>
        {t('privateNode:privateNode.manage')}
      </Typography>

      {/* 购买专属线路入口 — 专属线路套餐只在 product=private_node 端点售卖，
          绝不混入默认购买页（/api/plans 冻结为 app-only）。 */}
      <Button
        variant="contained"
        fullWidth
        onClick={() => navigate('/purchase?product=private_node')}
        sx={{ mb: 2, borderRadius: 2, fontWeight: 700 }}
      >
        {t('privateNode:privateNode.buyLine')}
      </Button>

      {loading ? (
        <Box sx={{ display: 'flex', justifyContent: 'center', py: 6 }}>
          <CircularProgress size={32} />
        </Box>
      ) : nodes.length === 0 ? (
        <Box sx={{ display: 'flex', justifyContent: 'center', py: 6 }}>
          <Typography variant="body2" color="text.secondary">
            {t('privateNode:privateNode.empty')}
          </Typography>
        </Box>
      ) : (
        nodes.map((node) => <PrivateNodePanel key={node.id} node={node} />)
      )}

      <AddRouterCard />
    </Box>
  );
}

/**
 * PrivateNodePanel — 单个专属节点订阅的状态卡片
 *
 * 一张 MUI 卡片展示一个专属节点订阅：状态、地区/节点 IP、IP 类型、
 * 流量用量（进度条 + 百分比）、到期时间、以及状态相关提示。
 *
 * 设计约束（webapp/CLAUDE.md）：
 * - 全部文案走 i18n（privateNode 命名空间，NESTED）
 * - 仅 MUI 暗色主题，禁止 window.confirm/alert/prompt
 * - 错误/状态映射不读后端 message
 *
 * 「续费」目前仅导航到既有 /purchase 流程（复用购买路径）。
 * 真正延长现有订阅 ExpiresAt 的续费是后续 Center 侧能力（Plan 5+），
 * 此处不实现任何支付逻辑，也不伪造续费结果。
 */

import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import {
  Card,
  CardContent,
  Stack,
  Box,
  Typography,
  Chip,
  LinearProgress,
  CircularProgress,
  Button,
  Alert,
} from '@mui/material';
import type { PrivateNodeSubscriptionView } from '../services/api-types';
import { formatBytes } from '../utils/ui';
import { formatDate } from '../utils/time';

type ChipColor = 'success' | 'warning' | 'error' | 'info' | 'default';

function statusChipColor(status: PrivateNodeSubscriptionView['status']): ChipColor {
  switch (status) {
    case 'active':
      return 'success';
    case 'grace':
      return 'warning';
    case 'suspended':
    case 'failed':
      return 'error';
    case 'pending':
    case 'provisioning':
      return 'info';
    case 'deprovisioned':
    default:
      return 'default';
  }
}

interface PrivateNodePanelProps {
  node: PrivateNodeSubscriptionView;
}

export function PrivateNodePanel({ node }: PrivateNodePanelProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();

  // pending/provisioning 还没有实例 → 不展示流量条
  const isProvisioning = node.status === 'pending' || node.status === 'provisioning';

  const percent = useMemo(() => {
    if (node.trafficTotalBytes <= 0) return 0;
    const p = (node.trafficUsedBytes / node.trafficTotalBytes) * 100;
    return Math.min(100, Math.max(0, p));
  }, [node.trafficUsedBytes, node.trafficTotalBytes]);

  const barColor: 'primary' | 'error' = percent >= 95 ? 'error' : 'primary';
  const ipTypeLabel = t(`privateNode:privateNode.ipType.${node.ipType}`, node.ipType);

  return (
    <Card sx={{ mb: 2 }}>
      <CardContent sx={{ py: 2, px: 2, '&:last-child': { pb: 2 } }}>
        <Stack spacing={1.5}>
          {/* Header: plan label + status chip */}
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 1 }}>
            <Typography variant="subtitle1" fontWeight={600} sx={{ fontSize: '0.95rem' }}>
              {node.planLabel || t('privateNode:privateNode.title')}
            </Typography>
            <Chip
              label={t(`privateNode:privateNode.status.${node.status}`)}
              color={statusChipColor(node.status)}
              size="small"
              sx={{ fontWeight: 600, fontSize: '0.7rem', height: 22 }}
            />
          </Box>

          {/* Region (+ node IP when provisioned) */}
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
            <Typography variant="caption" color="text.secondary">
              {t('privateNode:privateNode.region')}
            </Typography>
            <Typography variant="body2" fontWeight={600} sx={{ fontSize: '0.85rem' }}>
              {node.node?.region || node.region}
            </Typography>
            {node.node?.ip && (
              <>
                <Typography variant="caption" color="text.secondary" sx={{ ml: 1 }}>
                  {t('privateNode:privateNode.nodeIp')}
                </Typography>
                <Typography variant="body2" fontWeight={600} sx={{ fontSize: '0.85rem' }}>
                  {node.node.ip}
                </Typography>
              </>
            )}
          </Box>

          {/* IP type */}
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <Typography variant="caption" color="text.secondary">
              {t('privateNode:privateNode.ipTypeLabel')}
            </Typography>
            <Typography variant="body2" sx={{ fontSize: '0.85rem' }}>
              {ipTypeLabel}
            </Typography>
          </Box>

          {/* Traffic usage — only when an instance exists */}
          {isProvisioning ? (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, py: 0.5 }}>
              <CircularProgress size={18} />
              <Typography variant="body2" color="text.secondary" sx={{ fontSize: '0.85rem' }}>
                {t('privateNode:privateNode.provisioningHint')}
              </Typography>
            </Box>
          ) : (
            <Box>
              <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 0.5 }}>
                <Typography variant="caption" color="text.secondary">
                  {t('privateNode:privateNode.traffic')}
                </Typography>
                <Typography variant="caption" color={barColor === 'error' ? 'error' : 'text.secondary'}>
                  {t('privateNode:privateNode.trafficUsed', {
                    used: formatBytes(node.trafficUsedBytes),
                    total: formatBytes(node.trafficTotalBytes),
                  })}
                  {' · '}
                  {t('privateNode:privateNode.percentUsed', { percent: Math.round(percent) })}
                </Typography>
              </Box>
              <LinearProgress
                variant="determinate"
                value={percent}
                color={barColor}
                data-testid="private-node-traffic-bar"
                data-color={barColor}
                aria-valuenow={Math.round(percent)}
                sx={{ height: 6, borderRadius: 3 }}
              />
            </Box>
          )}

          {/* Expiry */}
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <Typography variant="caption" color="text.secondary">
              {t('privateNode:privateNode.expiresAt')}
            </Typography>
            <Typography variant="body2" fontWeight={600} sx={{ fontSize: '0.85rem' }}>
              {node.expiresAt > 0 ? formatDate(node.expiresAt) : '—'}
            </Typography>
          </Box>

          {/* Status-specific hints */}
          {node.status === 'grace' && (
            <Alert severity="warning" variant="outlined" sx={{ borderRadius: 1.5, py: 0.5 }}>
              {t('privateNode:privateNode.graceHint')}
            </Alert>
          )}
          {node.status === 'suspended' && (
            <Alert severity="error" variant="outlined" sx={{ borderRadius: 1.5, py: 0.5 }}>
              {t('privateNode:privateNode.suspendedHint')}
            </Alert>
          )}

          {/* Renew → reuse existing purchase flow (no payment logic here). */}
          <Button
            variant="outlined"
            color="primary"
            size="small"
            onClick={() => navigate('/purchase')}
            sx={{ alignSelf: 'flex-start', borderRadius: 1.5, textTransform: 'none', fontWeight: 600 }}
          >
            {t('privateNode:privateNode.renew')}
          </Button>
        </Stack>
      </CardContent>
    </Card>
  );
}

export default PrivateNodePanel;

import React from 'react';
import { 
  Box, 
  CircularProgress, 
  Typography, 
  Stack,
  Card,
  CardContent,
} from '@mui/material';
import {
  History as HistoryIcon,
  ShoppingBag as ShoppingBagIcon,
  Code as CodeIcon,
  Devices as DevicesIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';

// Loading 组件
interface LoadingStateProps {
  message?: string;
  size?: number;
  minHeight?: number;
}

export function LoadingState({ 
  message, 
  size = 40,
  minHeight = 200 
}: LoadingStateProps) {
  const { t } = useTranslation();
  const displayMessage = message || t('common:common.loading');
  return (
    <Box
      sx={{
        display: "flex",
        flexDirection: "column",
        justifyContent: "center",
        alignItems: "center",
        minHeight,
        gap: 2,
      }}
    >
      <CircularProgress size={size} thickness={4} />
      <Typography variant="body1" color="text.secondary">
        {displayMessage}
      </Typography>
    </Box>
  );
}

// Empty 状态组件
interface EmptyStateProps {
  icon?: React.ReactNode;
  title: string;
  description?: string;
  action?: React.ReactNode;
  minHeight?: number;
}

export function EmptyState({ 
  icon, 
  title, 
  description, 
  action,
  minHeight = 200 
}: EmptyStateProps) {
  return (
    <Box
      sx={{
        display: "flex",
        flexDirection: "column",
        justifyContent: "center",
        alignItems: "center",
        minHeight,
        gap: 2,
        p: 3,
      }}
    >
      {icon && (
        <Box sx={{ color: 'text.disabled', fontSize: 48 }}>
          {icon}
        </Box>
      )}
      <Stack spacing={1} alignItems="center">
        <Typography variant="h6" color="text.primary" fontWeight={600}>
          {title}
        </Typography>
        {description && (
          <Typography variant="body2" color="text.secondary" textAlign="center">
            {description}
          </Typography>
        )}
      </Stack>
      {action && action}
    </Box>
  );
}

// 特定场景的Empty组件
export function EmptyHistory() {
  const { t } = useTranslation();
  return (
    <EmptyState
      icon={<HistoryIcon sx={{ fontSize: 48 }} />}
      title={t('common:loadingAndEmpty.noHistoryTitle')}
      description={t('common:loadingAndEmpty.noHistoryDescription')}
    />
  );
}

export function EmptyPlans() {
  const { t } = useTranslation();
  return (
    <EmptyState
      icon={<ShoppingBagIcon sx={{ fontSize: 48 }} />}
      title={t('common:loadingAndEmpty.noPlansTitle')}
      description={t('common:loadingAndEmpty.noPlansDescription')}
    />
  );
}

export function EmptyInviteCodes() {
  const { t } = useTranslation();
  return (
    <EmptyState
      icon={<CodeIcon sx={{ fontSize: 48 }} />}
      title={t('common:loadingAndEmpty.noInviteCodesTitle')}
      description={t('common:loadingAndEmpty.noInviteCodesDescription')}
    />
  );
}

export function EmptyDevices() {
  const { t } = useTranslation();
  return (
    <EmptyState
      icon={<DevicesIcon sx={{ fontSize: 48 }} />}
      title={t('common:loadingAndEmpty.noDevicesTitle')}
      description={t('common:loadingAndEmpty.noDevicesDescription')}
    />
  );
}

// 带卡片样式的Loading组件（用于替换现有内容区域）
export function LoadingCard({ message }: { message?: string }) {
  return (
    <Card>
      <CardContent>
        <LoadingState message={message} minHeight={300} />
      </CardContent>
    </Card>
  );
}

// 带卡片样式的Empty组件
export function EmptyCard({ children }: { children: React.ReactNode }) {
  return (
    <Card>
      <CardContent>
        {children}
      </CardContent>
    </Card>
  );
} 
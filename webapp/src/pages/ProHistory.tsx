import { useState, useEffect } from "react";
import {
  Box,
  Typography,
  Chip,
  IconButton,
  Tooltip,
  Stack,
  List,
  ListItem,
  ListItemText,
  Divider,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router-dom";

import type { ProHistory, Pagination } from "../services/api-types";
import ContentCopyIcon from '@mui/icons-material/ContentCopy';
import BackButton from '../components/BackButton';
import { formatTime } from '../utils/time';
import { useTheme } from '@mui/material/styles';
import React from 'react';
import { LoadingCard, EmptyHistory } from '../components/LoadingAndEmpty';
import { useAlert } from "../stores";
import Pagit from "../components/Pagit";
import { k2api } from '../services/k2api';

export default function ProHistory() {
  const theme = useTheme();
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const typeFilter = searchParams.get('type') || undefined; // 从 URL 读取 type 参数
  const fromPage = searchParams.get('from') || '/account'; // 从 URL 读取 from 参数，默认返回 account

  const [histories, setHistories] = useState<ProHistory[]>([]);
  const [pagination, setPagination] = useState<Pagination>({
    page: 0,
    pageSize: 10,
    total: 0
  });
  const [loading, setLoading] = useState(false);
  const { showAlert } = useAlert();

  const fetchHistories = async (page = 0) => {
    setLoading(true);
    try {
      const params = new URLSearchParams({
        page: page.toString(),
        pageSize: pagination.pageSize.toString(),
      });
      if (typeFilter) {
        params.append('type', typeFilter);
      }
      const {code, data, message} = await k2api().exec<{ items: ProHistory[]; pagination: Pagination }>('api_request', {
        method: 'GET',
        path: `/api/user/pro-histories?${params.toString()}`,
      });
      console.debug(`[ProHistory] code: ${code} data: ` + JSON.stringify(data));
      if (code === 0) {
        setHistories(data?.items || []);
        if (data?.pagination) {
          setPagination(data.pagination);
        }
      } else {
        showAlert(message || t('account:proHistory.fetchFailed'), 'error');
      }
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : String(error);
      console.error(`获取 Pro/VIP 历史失败: error=${errorMessage}`);
      showAlert(t('account:proHistory.fetchFailedNetwork'), 'error');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchHistories();
    // eslint-disable-next-line
  }, [typeFilter]); // 当 typeFilter 变化时重新加载

  const handleCopy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      showAlert(t('account:proHistory.orderNumberCopied'), 'success');
    } catch {
      showAlert(t('account:proHistory.copyFailed'), 'error');
    }
  };

  const handlePageChange = (page: number) => {
    fetchHistories(page);
  };

  return (
    <Box sx={{
      width: "100%",
      py: 0.5,
      backgroundColor: "transparent",
      position: "relative"
    }}>
      <BackButton to={fromPage} />
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 1.5, px: 1, pt: 7 }}>
        <Typography
          variant="h6"
          sx={{
            flex: 1,
            fontWeight: 600,
            fontSize: '1.1rem' // Slightly smaller for narrow screen
          }}
          component="span"
        >
          {t('account:proHistory.title')}
        </Typography>
      </Box>

      {loading ? (
        <Box sx={{ px: 1 }}>
          <LoadingCard message={t('account:proHistory.loading')} />
        </Box>
      ) : histories.length === 0 ? (
        <Box sx={{ px: 1, backgroundColor: (theme) => theme.palette.background.paper, borderRadius: 2 }}>
          <EmptyHistory />
        </Box>
      ) : (
        <Box sx={{ backgroundColor: (theme) => theme.palette.background.paper, borderRadius: 2 }}>
            <List sx={{ py: 0.5 }}>
              {histories.map((h, idx) => (
                <React.Fragment key={h.createdAt + h.type + (h.order?.uuid || '')}>
                  <ListItem
                    alignItems="flex-start"
                    sx={{
                      display: 'block',
                      py: 1,
                      px: 1.5,
                      '&:hover': {
                        bgcolor: 'action.hover',
                        borderRadius: 1,
                      },
                      transition: 'background-color 0.2s ease',
                    }}
                  >
                    <ListItemText
                      primary={
                        <Stack
                          direction="column"
                          spacing={0.5}
                          sx={{ mb: h.order ? 1 : 0 }}
                          component="span"
                        >
                          {/* First row: Type chip and Days chip */}
                          <Stack
                            direction="row"
                            alignItems="center"
                            justifyContent="space-between"
                            component="span"
                          >
                            <Chip
                              label={h.type === 'recharge' ? t('account:proHistory.recharge') : h.type === 'reward' ? t('account:proHistory.reward') : h.type}
                              color={h.type === 'recharge' ? 'primary' : 'info'}
                              size="small"
                              component="span"
                              sx={{
                                fontWeight: 600,
                                fontSize: '0.7rem',
                                height: '22px',
                              }}
                            />
                            <Chip
                              label={`+${h.days}${t('account:proHistory.daysLabel')}`}
                              color="success"
                              size="small"
                              variant="outlined"
                              sx={{
                                fontWeight: 700,
                                fontSize: '0.7rem',
                                height: '22px',
                              }}
                              component="span"
                            />
                          </Stack>

                          {/* Second row: Reason (if exists) */}
                          {h.reason && (
                            <Typography
                              variant="body2"
                              color="text.secondary"
                              sx={{
                                fontSize: '0.8rem',
                                overflow: 'hidden',
                                textOverflow: 'ellipsis',
                                display: '-webkit-box',
                                WebkitLineClamp: 2,
                                WebkitBoxOrient: 'vertical',
                              }}
                              component="span"
                            >
                              {h.reason}
                            </Typography>
                          )}

                          {/* Third row: Timestamp */}
                          <Typography
                            variant="caption"
                            color="text.secondary"
                            sx={{ fontSize: '0.7rem' }}
                            component="span"
                          >
                            {formatTime(h.createdAt)}
                          </Typography>
                        </Stack>
                      }
                      secondary={
                        h.order && h.order.uuid && (
                          <Box component="span" sx={{
                            background: theme.palette.action.hover,
                            borderRadius: 1,
                            px: 1.5,
                            py: 1,
                            mt: 1,
                            mb: 0.5,
                            display: 'block',
                          }}>
                            {/* Order number row */}
                            <Stack
                              direction="row"
                              alignItems="center"
                              justifyContent="space-between"
                              sx={{ mb: 0.75 }}
                              component="span"
                            >
                              <Stack
                                direction="row"
                                alignItems="center"
                                spacing={0.5}
                                sx={{ flex: 1, minWidth: 0 }}
                                component="span"
                              >
                                <Typography
                                  variant="caption"
                                  sx={{
                                    fontFamily: 'monospace',
                                    fontWeight: 600,
                                    color: 'primary.main',
                                    fontSize: '0.7rem',
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                    whiteSpace: 'nowrap',
                                  }}
                                  component="span"
                                >
                                  {h.order.uuid}
                                </Typography>
                                <Tooltip title={t('account:proHistory.copyOrderNumber')} arrow>
                                  <IconButton
                                    size="small"
                                    onClick={() => handleCopy(h.order!.uuid)}
                                    component="span"
                                    sx={{
                                      padding: '2px',
                                      '&:hover': {
                                        bgcolor: 'primary.light',
                                        color: 'primary.contrastText',
                                      }
                                    }}
                                  >
                                    <ContentCopyIcon sx={{ fontSize: '0.9rem' }} />
                                  </IconButton>
                                </Tooltip>
                              </Stack>
                              <Chip
                                label={h.order.isPaid ? t('account:proHistory.paid') : t('account:proHistory.unpaid')}
                                color={h.order.isPaid ? "success" : "warning"}
                                size="small"
                                sx={{
                                  fontWeight: 600,
                                  flexShrink: 0,
                                  fontSize: '0.65rem',
                                  height: '20px',
                                  ml: 1
                                }}
                                component="span"
                              />
                            </Stack>

                            {/* Order details - vertical layout for narrow screen */}
                            <Stack direction="column" spacing={0.5} component="span">
                              {/* Product title */}
                              <Typography
                                variant="body2"
                                color="primary.main"
                                fontWeight={600}
                                component="span"
                                sx={{ fontSize: '0.8rem' }}
                              >
                                {h.order.title}
                              </Typography>

                              {/* Amount - highlighted */}
                              <Typography
                                variant="body2"
                                color="error.main"
                                fontWeight={700}
                                component="span"
                                sx={{ fontSize: '0.85rem' }}
                              >
                                ¥{(h.order.payAmount / 100).toFixed(2)}
                              </Typography>

                              {/* Pay time */}
                              <Typography
                                variant="caption"
                                color="text.secondary"
                                component="span"
                                sx={{ fontSize: '0.7rem' }}
                              >
                                {t('account:proHistory.payTime')}：{h.order.payAt ? formatTime(h.order.payAt) : "-"}
                              </Typography>

                              {/* Campaign info if exists */}
                              {h.order.campaign && (
                                <Stack direction="row" alignItems="center" spacing={1} component="span">
                                  <Chip
                                    key={h.order.campaign.id}
                                    label={h.order.campaign.description}
                                    color="info"
                                    size="small"
                                    sx={{
                                      fontWeight: 600,
                                      fontSize: '0.65rem',
                                      height: '20px'
                                    }}
                                    component="span"
                                  />
                                  <Typography
                                    variant="caption"
                                    color="success.main"
                                    fontWeight={600}
                                    component="span"
                                    sx={{ fontSize: '0.7rem' }}
                                  >
                                    {t('account:proHistory.discount')}：-¥{(h.order.campaignReduceAmount / 100).toFixed(2)}
                                  </Typography>
                                </Stack>
                              )}
                            </Stack>
                          </Box>
                        )
                      }
                    />
                  </ListItem>
                  {idx < histories.length - 1 && (
                    <Divider
                      key={h.createdAt + h.type + (h.order?.uuid || '') + '-divider'}
                      sx={{ mx: 1.5 }}
                    />
                  )}
                </React.Fragment>
              ))}
            </List>

            <Box sx={{ px: 2, pb: 1 }}>
              <Pagit
                pagination={pagination}
                onChange={handlePageChange}
                disabled={loading}
              />
            </Box>
        </Box>
      )}
    </Box>
  );
}

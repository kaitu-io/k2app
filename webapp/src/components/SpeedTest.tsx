import { useState, useEffect, useRef, useCallback } from "react";
import {
  Box,
  Card,
  CardContent,
  Button,
  CircularProgress,
  Alert,
  Paper,
  Grid,
  Divider,
  Stack,
  LinearProgress,
  Typography,
  IconButton,
} from "@mui/material";
import {
  Speed as SpeedIcon,
  CheckCircle as CheckCircleIcon,
  Error as ErrorIcon,
  Close as CloseIcon,
} from "@mui/icons-material";
import { useTranslation } from "react-i18next";

import type {
  SpeedtestProgress,
  SpeedtestResult,
} from "../services/control-types";

const POLL_INTERVAL = 500; // 500ms 轮询间隔

export default function SpeedTest() {
  const { t } = useTranslation();
  const [isTesting, setIsTesting] = useState(false);
  const [progress, setProgress] = useState<SpeedtestProgress | null>(null);
  const [result, setResult] = useState<SpeedtestResult | null>(null);
  const [error, setError] = useState<string | null>(null);

  // 轮询定时器引用
  const pollTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // 停止轮询
  const stopPolling = useCallback(() => {
    if (pollTimerRef.current) {
      clearInterval(pollTimerRef.current);
      pollTimerRef.current = null;
    }
  }, []);

  // 轮询测速状态
  const pollStatus = useCallback(async () => {
    try {
      const response = await window._k2.run('get_speedtest_status');

      if (response.code !== 0 || !response.data) {
        console.warn('[SpeedTest] Failed to get status:', response.message);
        return;
      }

      const data = response.data;

      // 更新进度
      if (data.progress) {
        setProgress(data.progress);
      }

      // 检查是否完成或出错
      if (data.status === 'completed' || data.status === 'error') {
        stopPolling();
        setIsTesting(false);
        setProgress(null);

        if (data.result) {
          setResult(data.result);
          if (!data.result.success) {
            setError(data.result.error || t('dashboard:troubleshooting.speedtest.failed'));
          }
        }
      }
    } catch (err) {
      console.error('[SpeedTest] Poll error:', err);
      // 轮询错误不中断，继续尝试
    }
  }, [stopPolling, t]);

  // 开始轮询
  const startPolling = useCallback(() => {
    stopPolling(); // 确保不重复
    pollTimerRef.current = setInterval(pollStatus, POLL_INTERVAL);
  }, [pollStatus, stopPolling]);

  // 组件卸载时停止轮询
  useEffect(() => {
    return () => {
      stopPolling();
    };
  }, [stopPolling]);

  const runSpeedTest = async () => {
    // 清除之前的结果和错误
    setResult(null);
    setProgress(null);
    setError(null);
    setIsTesting(true);

    try {
      const response = await window._k2.run('speedtest', { forceDirect: true });

      if (response.code !== 0) {
        console.error('[SpeedTest] Failed to start speedtest:', response.code, response.message);
        setError(t('dashboard:troubleshooting.speedtest.startFailed'));
        setIsTesting(false);
        return;
      }

      // 启动轮询
      startPolling();
    } catch (err: any) {
      setError(t('dashboard:troubleshooting.speedtest.networkError'));
      setIsTesting(false);
      setProgress(null);
    }
  };

  const formatSpeed = (mbps?: number) => {
    return mbps ? mbps.toFixed(2) : "0.00";
  };

  const formatLatency = (ms?: number) => {
    return ms ? `${ms}ms` : "N/A";
  };

  const formatDuration = (ms?: number) => {
    return ms ? (ms / 1000).toFixed(2) : "0";
  };

  const resetResult = () => {
    setResult(null);
    setError(null);
    setProgress(null);
  };

  // 如果有测试结果，显示结果视图
  if (!isTesting && result) {
    return (
      <Card>
        <CardContent sx={{ pb: 1.5 }}>
          <Stack spacing={1.2}>
            {/* 标题栏和关闭按钮 */}
            <Box display="flex" alignItems="center" justifyContent="space-between">
              <Box display="flex" alignItems="center" gap={1}>
                {result.success ? (
                  <CheckCircleIcon color="success" />
                ) : (
                  <ErrorIcon color="error" />
                )}
                <Typography variant="h6">
                  {result.success ? t('dashboard:troubleshooting.speedtest.complete') : t('dashboard:troubleshooting.speedtest.failed')}
                </Typography>
              </Box>
              <IconButton size="small" onClick={resetResult}>
                <CloseIcon />
              </IconButton>
            </Box>

            {result.success ? (
              <>
                <Divider />
                <Grid container spacing={1.5}>
                  <Grid item xs={6}>
                    <Typography variant="body2" color="text.secondary">
                      {t('dashboard:troubleshooting.speedtest.downloadSpeed')}
                    </Typography>
                    <Typography variant="h6" color="primary">
                      {formatSpeed(result.download_mbps)} Mbps
                    </Typography>
                  </Grid>
                  <Grid item xs={6}>
                    <Typography variant="body2" color="text.secondary">
                      {t('dashboard:troubleshooting.speedtest.uploadSpeed')}
                    </Typography>
                    <Typography variant="h6" color="primary">
                      {formatSpeed(result.upload_mbps)} Mbps
                    </Typography>
                  </Grid>
                  <Grid item xs={6}>
                    <Typography variant="body2" color="text.secondary">
                      {t('dashboard:troubleshooting.speedtest.latency')}
                    </Typography>
                    <Typography variant="body1">
                      {formatLatency(result.latency_ms)}
                    </Typography>
                  </Grid>
                  <Grid item xs={6}>
                    <Typography variant="body2" color="text.secondary">
                      {t('dashboard:troubleshooting.speedtest.jitter')}
                    </Typography>
                    <Typography variant="body1">
                      {formatLatency(result.jitter_ms)}
                    </Typography>
                  </Grid>
                  <Grid item xs={12}>
                    <Typography variant="body2" color="text.secondary">
                      {t('dashboard:troubleshooting.speedtest.duration')}
                    </Typography>
                    <Typography variant="body1">
                      {formatDuration(result.duration_ms)} {t('dashboard:troubleshooting.speedtest.seconds')}
                    </Typography>
                  </Grid>
                </Grid>
                <Divider />
                <Typography variant="caption" color="text.secondary">
                  {t('dashboard:troubleshooting.speedtest.server')}: {result.server_name || "N/A"}
                </Typography>
              </>
            ) : (
              <Alert severity="error">
                {t('dashboard:troubleshooting.speedtest.failed')}
              </Alert>
            )}
          </Stack>
        </CardContent>
      </Card>
    );
  }

  // 默认视图：测速开始界面或测试中
  return (
    <Card>
      <CardContent>
        <Stack spacing={2}>
          <Box display="flex" alignItems="center" gap={1}>
            <SpeedIcon color="primary" />
            <Typography variant="h6">{t('dashboard:troubleshooting.speedtest.title')}</Typography>
          </Box>

          <Typography variant="body2" color="text.secondary">
            {t('dashboard:troubleshooting.speedtest.description')}
          </Typography>

          <Button
            variant="contained"
            onClick={runSpeedTest}
            disabled={isTesting}
            startIcon={isTesting ? <CircularProgress size={20} /> : <SpeedIcon />}
            fullWidth
          >
            {isTesting ? t('dashboard:troubleshooting.speedtest.testing') : t('dashboard:troubleshooting.speedtest.start')}
          </Button>

          {/* 错误提示 */}
          {error && (
            <Alert severity="error" onClose={() => setError(null)}>
              {error}
            </Alert>
          )}

          {/* 进度显示 */}
          {isTesting && progress && (
            <Paper variant="outlined" sx={{ p: 2 }}>
              <Stack spacing={1}>
                <Typography variant="body2" color="text.secondary">
                  {progress.message}
                </Typography>
                <LinearProgress variant="determinate" value={progress.percentage} />
                <Typography variant="caption" color="text.secondary">
                  {progress.percentage}%
                  {progress.current_speed && progress.current_speed > 0 && (
                    <> · {t('dashboard:troubleshooting.speedtest.currentSpeed')}: {formatSpeed(progress.current_speed)} Mbps</>
                  )}
                </Typography>
              </Stack>
            </Paper>
          )}
        </Stack>
      </CardContent>
    </Card>
  );
}

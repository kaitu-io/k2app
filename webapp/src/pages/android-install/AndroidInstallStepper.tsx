import { useState, useEffect, useRef, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Stepper,
  Step,
  StepLabel,
  StepContent,
  Button,
  Typography,
  Chip,
  Stack,
  CircularProgress,
  Alert,
  Card,
  CardMedia,
} from '@mui/material';
import {
  PhoneAndroid as PhoneIcon,
  Usb as UsbIcon,
  CheckCircle as CheckIcon,
  Refresh as RetryIcon,
  Error as ErrorIcon,
} from '@mui/icons-material';
import { brandGuides } from './adb-guide-data';

interface Props {
  name: string;
  icon: string;
  desc: string;
  apkUrl: string; // "" = Kaitu default, non-empty = direct URL
}

type InstallPhase = 'idle' | 'prepare_adb' | 'downloading' | 'pushing' | 'installing' | 'done' | 'error';

interface InstallStatus {
  phase: InstallPhase;
  progress: number;
  version: string;
  error: string;
}

interface DeviceInfo {
  serial: string;
  state: string;
  model: string;
}

interface DetectResponse {
  adb_ready: boolean;
  devices: DeviceInfo[];
  installing_driver: boolean;
}

export default function AndroidInstallStepper({ name, icon, desc, apkUrl }: Props) {
  const { t } = useTranslation();
  const [activeStep, setActiveStep] = useState(0);
  const [selectedBrand, setSelectedBrand] = useState<string | null>(null);
  const [devices, setDevices] = useState<DeviceInfo[]>([]);
  const [detecting, setDetecting] = useState(false);
  const [installingDriver, setInstallingDriver] = useState(false);
  const [installStatus, setInstallStatus] = useState<InstallStatus>({
    phase: 'idle', progress: 0, version: '', error: '',
  });
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const detectPollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const detectingRef = useRef(false);

  // Step 2: Detect devices with concurrency guard
  const detectDevices = useCallback(async () => {
    if (!window._k2 || detectingRef.current) return;
    detectingRef.current = true;
    setDetecting(true);
    try {
      const resp = await window._k2.run<DetectResponse>('adb-detect', {});
      if (resp.code === 0 && resp.data) {
        setDevices(resp.data.devices || []);
        setInstallingDriver(resp.data.installing_driver || false);
        if (resp.data.devices?.length > 0) {
          stopDetectPolling();
          setActiveStep(2);
        }
      }
    } catch (e) {
      console.error('detect failed', e);
    } finally {
      detectingRef.current = false;
      setDetecting(false);
    }
  }, []);

  const stopDetectPolling = useCallback(() => {
    if (detectPollRef.current) {
      clearInterval(detectPollRef.current);
      detectPollRef.current = null;
    }
  }, []);

  const startDetectPolling = useCallback(() => {
    stopDetectPolling();
    detectDevices();
    detectPollRef.current = setInterval(detectDevices, 3000);
  }, [detectDevices, stopDetectPolling]);

  // Step 4: Start install
  const startInstall = useCallback(async () => {
    if (!window._k2) return;
    const serial = devices.length === 1 ? devices[0].serial : '';
    try {
      const resp = await window._k2.run('adb-install', { url: apkUrl, serial });
      if (resp.code === 0) {
        // Start polling status
        pollRef.current = setInterval(async () => {
          const statusResp = await window._k2!.run<InstallStatus>('adb-status', {});
          if (statusResp.code === 0 && statusResp.data) {
            setInstallStatus(statusResp.data);
            if (statusResp.data.phase === 'done' || statusResp.data.phase === 'error') {
              if (pollRef.current) clearInterval(pollRef.current);
              pollRef.current = null;
            }
          }
        }, 500);
      }
    } catch (e) {
      console.error('install failed', e);
    }
  }, [devices, apkUrl]);

  // Auto-poll when on Step 2
  useEffect(() => {
    if (activeStep === 1) {
      startDetectPolling();
    } else {
      stopDetectPolling();
    }
    return stopDetectPolling;
  }, [activeStep, startDetectPolling, stopDetectPolling]);

  // Cleanup all polling on unmount
  useEffect(() => {
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
      if (detectPollRef.current) clearInterval(detectPollRef.current);
    };
  }, []);

  const phaseLabel = (phase: InstallPhase): string => {
    const map: Record<string, string> = {
      idle: '',
      prepare_adb: t('purchase:androidInstall.preparingAdb'),
      downloading: t('purchase:androidInstall.downloading'),
      pushing: t('purchase:androidInstall.pushing'),
      installing: t('purchase:androidInstall.installingOnDevice'),
      done: t('purchase:androidInstall.done'),
      error: t('purchase:androidInstall.installFailed'),
    };
    return map[phase] || phase;
  };

  const isInstalling = installStatus.phase !== 'idle' && installStatus.phase !== 'done' && installStatus.phase !== 'error';

  const steps = [
    // Step 1: Enable Developer Options + USB Debugging
    {
      label: t('purchase:androidInstall.step1Title'),
      content: (
        <Box>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {t('purchase:androidInstall.step1Desc')}
          </Typography>

          <Typography variant="body2" sx={{ mb: 1.5, fontWeight: 500 }}>
            {t('purchase:androidInstall.selectBrand')}
          </Typography>

          <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap sx={{ mb: 2 }}>
            {brandGuides.map((brand) => (
              <Chip
                key={brand.id}
                label={t(`purchase:androidInstall.${brand.nameKey}`)}
                onClick={() => setSelectedBrand(brand.id)}
                variant={selectedBrand === brand.id ? 'filled' : 'outlined'}
                color={selectedBrand === brand.id ? 'primary' : 'default'}
                sx={{ mb: 0.5 }}
              />
            ))}
            <Chip
              label={t('purchase:androidInstall.brandOther')}
              onClick={() => setSelectedBrand('other')}
              variant={selectedBrand === 'other' ? 'filled' : 'outlined'}
              color={selectedBrand === 'other' ? 'primary' : 'default'}
              sx={{ mb: 0.5 }}
            />
          </Stack>

          {selectedBrand === 'huawei' && (
            <Alert severity="warning" sx={{ mb: 2 }}>
              <Typography variant="body2" fontWeight={600} sx={{ mb: 0.5 }}>
                {t('purchase:androidInstall.huaweiWarningTitle')}
              </Typography>
              <Typography variant="body2">
                {t('purchase:androidInstall.huaweiWarningDesc')}
              </Typography>
            </Alert>
          )}

          {selectedBrand && selectedBrand !== 'other' && (
            <BrandGuideImages brandId={selectedBrand} />
          )}

          {selectedBrand === 'other' && (
            <Alert severity="info" sx={{ mb: 2 }}>
              {t('purchase:androidInstall.otherBrandHint')}
            </Alert>
          )}

          <Stack direction="row" spacing={1} sx={{ mt: 1 }}>
            <Button variant="contained" onClick={() => setActiveStep(1)}>
              {t('common:common.next', '下一步')}
            </Button>
          </Stack>
        </Box>
      ),
    },

    // Step 2: USB Connect & Authorize (auto-polls every 3s)
    {
      label: t('purchase:androidInstall.step2Title'),
      content: (
        <Box>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
            {t('purchase:androidInstall.step2Desc')}
          </Typography>
          <Typography variant="body2" color="warning.main" sx={{ mb: 2 }}>
            {t('purchase:androidInstall.usbTrustPrompt')}
          </Typography>

          {devices.length === 0 && (
            <Stack direction="row" alignItems="center" spacing={1.5} sx={{ mb: 2 }}>
              <CircularProgress size={20} />
              <Typography variant="body2" color="text.secondary">
                {installingDriver
                  ? t('purchase:androidInstall.installingDriver')
                  : t('purchase:androidInstall.scanningDevice')}
              </Typography>
            </Stack>
          )}

          {devices.length > 0 && (
            <Stack spacing={1} sx={{ mb: 2 }}>
              {devices.map((d) => (
                <Chip
                  key={d.serial}
                  icon={<PhoneIcon />}
                  label={`${d.model || d.serial} (${d.state})`}
                  color="success"
                  variant="outlined"
                />
              ))}
            </Stack>
          )}

          <Stack direction="row" spacing={1}>
            <Button variant="text" onClick={() => setActiveStep(0)}>
              {t('common:common.back', '上一步')}
            </Button>
            {devices.length === 0 && (
              <Button
                variant="outlined"
                startIcon={<UsbIcon />}
                onClick={detectDevices}
                disabled={detecting}
                size="small"
              >
                {t('purchase:androidInstall.rescan')}
              </Button>
            )}
          </Stack>
        </Box>
      ),
    },

    // Step 3: Auto Install
    {
      label: t('purchase:androidInstall.step3Title'),
      content: (
        <Box>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {t('purchase:androidInstall.step3Desc')}
          </Typography>

          <Stack direction="row" alignItems="center" spacing={1.5} sx={{ mb: 2 }}>
            <Box component="img" src={icon} alt={name} sx={{ width: 40, height: 40, borderRadius: 1 }} />
            <Box>
              <Typography variant="body2" fontWeight={600}>{name}</Typography>
              {desc && <Typography variant="caption" color="text.secondary">{desc}</Typography>}
            </Box>
          </Stack>

          {installStatus.phase === 'idle' && (
            <Stack direction="row" spacing={1} sx={{ mb: 2 }}>
              <Button variant="text" onClick={() => setActiveStep(1)}>
                {t('common:common.back', '上一步')}
              </Button>
              <Button
                variant="contained"
                color="primary"
                onClick={startInstall}
                disabled={devices.length === 0}
              >
                {t('purchase:androidInstall.startInstall')}
              </Button>
            </Stack>
          )}

          {isInstalling && (
            <Stack direction="row" alignItems="center" spacing={2} sx={{ mb: 2 }}>
              <CircularProgress size={24} />
              <Box sx={{ flex: 1 }}>
                <Typography variant="body2">{phaseLabel(installStatus.phase)}</Typography>
                {installStatus.progress > 0 && (
                  <Typography variant="caption" color="text.secondary">
                    {installStatus.progress}%
                  </Typography>
                )}
              </Box>
            </Stack>
          )}

          {installStatus.phase === 'done' && (
            <Alert icon={<CheckIcon />} severity="success" sx={{ mb: 2 }}>
              {t('purchase:androidInstall.done')}
              {installStatus.version && ` (v${installStatus.version})`}
            </Alert>
          )}

          {installStatus.phase === 'error' && (
            <Box sx={{ mb: 2 }}>
              <Alert icon={<ErrorIcon />} severity="error" sx={{ mb: 1 }}>
                {installStatus.error || t('purchase:androidInstall.installFailed')}
              </Alert>
              <Button
                variant="outlined"
                startIcon={<RetryIcon />}
                onClick={() => {
                  setInstallStatus({ phase: 'idle', progress: 0, version: '', error: '' });
                  startInstall();
                }}
              >
                {t('purchase:androidInstall.retry')}
              </Button>
            </Box>
          )}
        </Box>
      ),
    },
  ];

  return (
    <Stepper activeStep={activeStep} orientation="vertical">
      {steps.map((step, index) => (
        <Step key={index} completed={activeStep > index || installStatus.phase === 'done'}>
          <StepLabel
            onClick={() => {
              // Allow clicking back to previous steps (but not forward past device detection)
              if (index <= activeStep) setActiveStep(index);
            }}
            sx={{ cursor: index <= activeStep ? 'pointer' : 'default' }}
          >
            {step.label}
          </StepLabel>
          <StepContent>{step.content}</StepContent>
        </Step>
      ))}
    </Stepper>
  );
}

// Sub-component: brand-specific guide images
function BrandGuideImages({ brandId }: { brandId: string }) {
  const { t } = useTranslation();
  const guide = brandGuides.find((g) => g.id === brandId);
  if (!guide) return null;

  return (
    <Stack spacing={2} sx={{ mb: 2 }}>
      {guide.steps.map((step, i) => (
        <Box key={i}>
          <Typography variant="body2" sx={{ mb: 0.5, fontWeight: 500 }}>
            {i + 1}. {t(`purchase:androidInstall.${step.titleKey}`)}
          </Typography>
          <Card
            sx={{
              maxWidth: 300,
              bgcolor: 'background.paper',
              border: '1px solid',
              borderColor: 'divider',
            }}
          >
            <CardMedia
              component="img"
              image={step.image}
              alt={t('purchase:androidInstall.guideImageAlt', { step: i + 1 })}
              sx={{ maxHeight: 500, objectFit: 'contain' }}
            />
          </Card>
        </Box>
      ))}
    </Stack>
  );
}

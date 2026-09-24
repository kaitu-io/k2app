'use client';

/**
 * 我的路由器 —— 账户侧的路由器版状态页（spec §4.4）：在线状态、开通进度、
 * 本月用量、服务到期与续费，以及自备路由器的安装凭证生成 / 重新生成 / 复制流程。
 *
 * 鉴权由 account/layout.tsx 的 useAuth 守卫承担，本组件假定已登录（同 KaituAccountClient）。
 */
import { useCallback, useEffect, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { Link } from '@/i18n/routing';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Progress } from '@/components/ui/progress';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { RouterCliBlock } from '@/app/[locale]/install/install-guides';
import { api, ApiError, type UserRouter } from '@/lib/api';
import { getApiErrorMessage } from '@/lib/api-errors';
import {
  buildInstallCommand,
  routerProgressSteps,
  quotaLevel,
  formatBytes,
  formatShipsFrom,
  routerOffer,
} from '@/lib/router-edition';

// stage 落在这些值时开通链路仍在异步推进（付款确认 / 开线路 / 发货），30 秒轮询一次；
// online / expired 是终态，继续轮询只会白耗请求。
const POLLING_STAGES = new Set(['paid', 'provisioning', 'ready', 'shipped']);
const POLL_MS = 30000;

// 页面标题包在所有状态（加载 / 失败 / 空 / 有路由器）外面，任何状态下都在。
// 用 h2：account/layout.tsx 已经渲染了本页唯一的 h1（账户标题）。
export default function RouterAccountClient() {
  const t = useTranslations();
  return (
    <div className="flex flex-col gap-4">
      <h2 className="text-2xl font-semibold">{t('routers.edition.account.title')}</h2>
      <RouterAccountBody />
    </div>
  );
}

function RouterAccountBody() {
  const t = useTranslations();
  const locale = useLocale();

  const [data, setData] = useState<UserRouter | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  // 凭证 URL 只放组件 state：不落 storage、不进日志——泄露等于给别人开路由器。
  const [credentialUrl, setCredentialUrl] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [minting, setMinting] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);

  const load = useCallback(async () => {
    const result = await api.getUserRouter({ autoRedirectToAuth: false });
    setData(result);
    setLoadFailed(false);
    return result;
  }, []);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    load()
      .catch((err) => {
        console.error('[RouterAccount] Failed to fetch router:', err);
        if (!cancelled) setLoadFailed(true);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [load]);

  // 开通链路仍在推进时静默轮询；刷新失败不覆盖已经渲染的数据（spec 第 3 点）。
  const stage = data?.fulfillment?.stage;
  useEffect(() => {
    if (!stage || !POLLING_STAGES.has(stage)) return;
    const id = setInterval(() => {
      api
        .getUserRouter({ autoRedirectToAuth: false })
        .then((result) => setData(result))
        .catch((err) => console.error('[RouterAccount] Poll refresh failed:', err));
    }, POLL_MS);
    return () => clearInterval(id);
  }, [stage]);

  const handleRetry = useCallback(() => {
    setLoading(true);
    load()
      .catch((err) => {
        console.error('[RouterAccount] Retry failed:', err);
        setLoadFailed(true);
      })
      .finally(() => setLoading(false));
  }, [load]);

  // regions.<slug> 键缺失时回落显示原始 slug（同 RouterPurchaseClient 的约定）。
  const regionLabel = useCallback(
    (slug: string) => {
      const key = `routers.edition.regions.${slug}`;
      return t.has(key) ? t(key) : slug;
    },
    [t],
  );

  const handleMint = useCallback(async () => {
    setMinting(true);
    try {
      const { url } = await api.mintGatewayCredential();
      setCredentialUrl(url);
      setCopied(false);
      // 成功后立即重新拉一次，让 credentialMinted / canMintCredential 等状态跟后端同步。
      await load().catch(() => {});
    } catch (err) {
      console.error('[RouterAccount] mintGatewayCredential failed:', err);
      toast.error(err instanceof ApiError ? getApiErrorMessage(err.code, t) : t('errors.unknown'));
    } finally {
      setMinting(false);
    }
  }, [load, t]);

  const handleConfirmRegenerate = useCallback(() => {
    setConfirmOpen(false);
    void handleMint();
  }, [handleMint]);

  const handleCopy = useCallback(async () => {
    if (!credentialUrl) return;
    const command = buildInstallCommand(window.location.origin, credentialUrl);
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      toast.success(t('routers.edition.account.copied'));
    } catch (err) {
      console.error('[RouterAccount] Clipboard write failed:', err);
    }
  }, [credentialUrl, t]);

  if (loading) {
    return (
      <div className="flex min-h-[300px] items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-2 border-primary border-t-transparent" />
      </div>
    );
  }

  if (loadFailed || !data) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center gap-4 py-10 text-center" data-testid="router-load-error">
          <p className="text-muted-foreground">{t('routers.edition.account.loadFailed')}</p>
          <Button variant="outline" onClick={handleRetry}>
            {t('routers.edition.account.retry')}
          </Button>
        </CardContent>
      </Card>
    );
  }

  if (!data.hasRouter) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center gap-4 py-10 text-center" data-testid="router-empty">
          <h2 className="text-lg font-semibold">{t('routers.edition.account.emptyTitle')}</h2>
          <p className="text-muted-foreground">{t('routers.edition.account.emptyBody')}</p>
          <Button asChild>
            <Link href="/routers">{t('routers.edition.account.emptyCta')}</Link>
          </Button>
        </CardContent>
      </Card>
    );
  }

  const { fulfillment, line, device, renewPlanPid } = data;
  const isByo = fulfillment?.hardwareSku === '';
  const isHardware = !!fulfillment && fulfillment.hardwareSku !== '';
  const isExpired = fulfillment?.stage === 'expired';
  const steps = fulfillment ? routerProgressSteps(fulfillment) : [];
  const usageLevel = line ? quotaLevel(line.trafficUsedBytes, line.trafficTotalBytes) : 'normal';
  const showInstallCard = isByo && !isExpired;
  const canGenerate = !!fulfillment?.canMintCredential && !fulfillment.credentialMinted && !credentialUrl;
  // 含硬件且尚未发货：线路上的 expiresAt 只是付款时的暂定值，发货时后端会按发货日重设——
  // 这之前不显示到期日，免得预售用户看到一个错的日期。
  const awaitingShipment = isHardware && !isExpired && fulfillment.shippedAt === 0;
  const offer = routerOffer();
  const presaleShipping = awaitingShipment && offer.presale;

  return (
    <div className="flex flex-col gap-6">
      {/* 状态卡 */}
      <Card>
        <CardContent className="flex flex-col gap-3 py-6">
          <div className="flex items-center gap-2">
            <span
              data-testid="router-online-dot"
              className={`h-2.5 w-2.5 rounded-full ${device?.online ? 'bg-green-500' : 'bg-muted-foreground'}`}
            />
            <span className="font-medium">
              {device?.online ? t('routers.edition.account.online') : t('routers.edition.account.offline')}
            </span>
          </div>
          {device && (
            <>
              <div className="flex items-center justify-between text-sm">
                <span className="text-muted-foreground">{t('routers.edition.account.version')}</span>
                <span>{device.appVersion}</span>
              </div>
              <div className="flex items-center justify-between text-sm">
                <span className="text-muted-foreground">{t('routers.edition.account.lastSeen')}</span>
                <span>
                  {device.lastSeenAt
                    ? new Date(device.lastSeenAt * 1000).toLocaleString(locale)
                    : t('routers.edition.account.neverConnected')}
                </span>
              </div>
            </>
          )}
          {line && (
            <div className="flex items-center justify-between text-sm">
              <span className="text-muted-foreground">{t('routers.edition.account.region')}</span>
              <span>{regionLabel(line.region)}</span>
            </div>
          )}
        </CardContent>
      </Card>

      {/* 开通进度卡 */}
      {fulfillment && (
        <Card>
          <CardHeader className="pb-0">
            <CardTitle>{t('routers.edition.account.progressTitle')}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3 pt-4">
            {isExpired ? (
              <p className="text-sm text-destructive">{t('routers.edition.account.expired')}</p>
            ) : (
              <>
                <div className="flex flex-wrap items-center gap-2">
                  {steps.map((step) => (
                    <span
                      key={step.key}
                      data-testid={`progress-step-${step.key}`}
                      data-state={step.state}
                      className={
                        step.state === 'done'
                          ? 'rounded-full bg-primary px-3 py-1 text-xs font-medium text-primary-foreground'
                          : step.state === 'current'
                            ? 'rounded-full bg-primary/20 px-3 py-1 text-xs font-medium text-primary'
                            : 'rounded-full bg-muted px-3 py-1 text-xs font-medium text-muted-foreground'
                      }
                    >
                      {t(`routers.edition.account.stage.${step.key}`)}
                    </span>
                  ))}
                </div>
                {isHardware && fulfillment.trackingNo && (
                  <p className="text-sm text-muted-foreground">
                    {t('routers.edition.account.tracking', {
                      carrier: fulfillment.carrier ?? '',
                      trackingNo: fulfillment.trackingNo,
                    })}
                  </p>
                )}
                {presaleShipping && (
                  <p data-testid="presale-shipping" className="text-sm text-primary">
                    {t('routers.edition.account.presaleShipping', { date: formatShipsFrom(offer.shipsFrom, locale) })}
                  </p>
                )}
                {isHardware && (
                  <p className="text-sm text-muted-foreground">{t('routers.edition.account.hardwareHint')}</p>
                )}
              </>
            )}
          </CardContent>
        </Card>
      )}

      {/* 用量卡 */}
      {line && (
        <Card>
          <CardHeader className="pb-0">
            <CardTitle>{t('routers.edition.account.usageTitle')}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3 pt-4">
            <div data-testid="usage-progress" data-level={usageLevel}>
              <Progress
                value={Math.min(100, (line.trafficUsedBytes / Math.max(line.trafficTotalBytes, 1)) * 100)}
                className={
                  usageLevel === 'danger'
                    ? '[&>div]:bg-destructive'
                    : usageLevel === 'warn'
                      ? '[&>div]:bg-yellow-500'
                      : undefined
                }
              />
            </div>
            <p className="text-sm text-muted-foreground">
              {t('routers.edition.account.usageText', {
                used: formatBytes(line.trafficUsedBytes),
                total: formatBytes(line.trafficTotalBytes),
              })}
            </p>
            {line.quotaExhausted && (
              <p className="text-sm text-destructive">
                {t('routers.edition.account.quotaExhausted', {
                  date: new Date(line.quotaResetAt * 1000).toLocaleDateString(locale),
                })}
              </p>
            )}
          </CardContent>
        </Card>
      )}

      {/* 订阅卡 */}
      {line && (
        <Card>
          <CardHeader className="pb-0">
            <CardTitle>{t('routers.edition.account.subscriptionTitle')}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3 pt-4">
            {awaitingShipment ? (
              <p data-testid="expires-after-ship" className="text-sm text-muted-foreground">
                {t('routers.edition.account.expiresAfterShip')}
              </p>
            ) : (
              <p className="text-sm">
                {t('routers.edition.account.expiresAt', {
                  date: new Date(line.expiresAt * 1000).toLocaleDateString(locale),
                })}
              </p>
            )}
            {(line.status === 'grace' || line.status === 'suspended') && (
              <p className="text-sm text-destructive">{t('routers.edition.account.overdue')}</p>
            )}
            {renewPlanPid && (
              <Button asChild>
                <Link href="/purchase/router?plan=svc">{t('routers.edition.account.renew')}</Link>
              </Button>
            )}
          </CardContent>
        </Card>
      )}

      {/* 安装卡：仅自备（含路由器成品从不显示）且未过期 */}
      {showInstallCard && (
        <Card>
          <CardHeader className="pb-0">
            <CardTitle>{t('routers.edition.account.installTitle')}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3 pt-4">
            {!fulfillment?.canMintCredential ? (
              <p className="text-sm text-muted-foreground">{t('routers.edition.account.installPending')}</p>
            ) : credentialUrl ? (
              <div className="flex flex-col gap-2">
                <RouterCliBlock
                  command={buildInstallCommand(window.location.origin, credentialUrl)}
                  onCopy={() => void handleCopy()}
                  copied={copied}
                />
                <p className="text-sm text-muted-foreground">{t('routers.edition.account.commandHint')}</p>
              </div>
            ) : canGenerate ? (
              <Button onClick={() => void handleMint()} disabled={minting}>
                {t('routers.edition.account.generate')}
              </Button>
            ) : (
              <>
                <p className="text-sm text-muted-foreground">{t('routers.edition.account.regenerateHint')}</p>
                <Button variant="outline" onClick={() => setConfirmOpen(true)} disabled={minting}>
                  {t('routers.edition.account.regenerate')}
                </Button>
              </>
            )}
          </CardContent>
        </Card>
      )}

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('routers.edition.account.regenerateConfirmTitle')}</AlertDialogTitle>
            <AlertDialogDescription>{t('routers.edition.account.regenerateConfirmBody')}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('routers.edition.account.cancel')}</AlertDialogCancel>
            <AlertDialogAction onClick={handleConfirmRegenerate}>
              {t('routers.edition.account.confirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

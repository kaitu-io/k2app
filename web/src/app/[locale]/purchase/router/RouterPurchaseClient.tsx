'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { Link, useRouter } from '@/i18n/routing';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import PurchaseStep1 from '@/components/PurchaseStep1';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { useAuth } from '@/contexts/AuthContext';
import { useEmbedMode } from '@/hooks/useEmbedMode';
import { api, ApiError, ErrorCode, type Order, type Plan, type RouterShipping } from '@/lib/api';
import { getApiErrorMessage } from '@/lib/api-errors';
import { formatUsd } from '@/lib/router-edition';

type Choice = 'hardware' | 'service';

const EMPTY_SHIPPING: RouterShipping = { name: '', phone: '', address: '' };

export default function RouterPurchaseClient() {
  const t = useTranslations();
  const router = useRouter();
  const searchParams = useSearchParams();
  const { isAuthenticated } = useAuth();
  const { showNavigation, showFooter } = useEmbedMode();

  const [plans, setPlans] = useState<Plan[]>([]);
  const [plansLoading, setPlansLoading] = useState(true);
  const [choice, setChoice] = useState<Choice>(searchParams.get('plan') === 'svc' ? 'service' : 'hardware');
  const [region, setRegion] = useState('');
  const [shipping, setShipping] = useState<RouterShipping>(EMPTY_SHIPPING);
  const [showCampaign, setShowCampaign] = useState(false);
  const [campaignCode, setCampaignCode] = useState('');
  const [campaignError, setCampaignError] = useState('');
  const [preview, setPreview] = useState<Order | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [alreadyHasRouter, setAlreadyHasRouter] = useState(false);
  const [payDialogOpen, setPayDialogOpen] = useState(false);
  const previewTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const hardwarePlan = useMemo(() => plans.find((p) => !!p.hardwareSku), [plans]);
  const servicePlan = useMemo(() => plans.find((p) => !p.hardwareSku), [plans]);
  const selected = choice === 'hardware' ? hardwarePlan : servicePlan;
  const regions = useMemo(() => selected?.privateNode?.allowedRegions ?? [], [selected]);
  const needsShipping = !!selected?.hardwareSku;
  const trimmedShipping: RouterShipping = {
    name: shipping.name.trim(),
    phone: shipping.phone.trim(),
    address: shipping.address.trim(),
  };
  const shippingComplete =
    !needsShipping ||
    (trimmedShipping.name !== '' && trimmedShipping.phone !== '' && trimmedShipping.address !== '');

  // 1) 拉套餐（hardwareSku 非空 = 成品，空 = 服务）。
  useEffect(() => {
    let cancelled = false;
    (async () => {
      setPlansLoading(true);
      try {
        const data = await api.getProductPlans('router', { autoRedirectToAuth: false });
        if (cancelled) return;
        setPlans(data.items ?? []);
      } catch (err) {
        console.error('[RouterPurchase] Failed to fetch plans:', err);
        if (cancelled) return;
        setPlans([]);
        toast.error(t('routers.edition.purchase.plansLoadFailed'));
      } finally {
        if (!cancelled) setPlansLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 2) 选中套餐变化 → region 不在允许列表时回落到首项。
  useEffect(() => {
    if (regions.length === 0) return;
    setRegion((prev) => (regions.includes(prev) ? prev : regions[0]));
  }, [regions]);

  // 一户一台门的提示只对应「刚才那次尝试」——切套餐或换地区说明用户已经在
  // 换一种方案重试，旧提示不该继续挂着。
  useEffect(() => {
    setAlreadyHasRouter(false);
  }, [choice, region]);

  // 5) regionLabel(slug)：键不存在时回落显示 slug。
  const regionLabel = useCallback(
    (slug: string) => {
      const key = `routers.edition.regions.${slug}`;
      return t.has(key) ? t(key) : slug;
    },
    [t],
  );

  // 3) plan / region / campaignCode 变化 → 500ms debounce 预览（不带 shipping）。
  const fetchPreview = useCallback(async () => {
    if (!selected) return;
    try {
      const { order } = await api.createOrder(
        {
          preview: true,
          plan: selected.pid,
          region: region || undefined,
          campaignCode: campaignCode || undefined,
        },
        { autoRedirectToAuth: false },
      );
      setPreview(order);
      setCampaignError('');
    } catch (err) {
      console.error('[RouterPurchase] Preview failed:', err);
      // 预览失败只清空预览金额，不弹 toast 骚扰——只有真实下单的错误才提示。
      if (err instanceof ApiError && err.code === ErrorCode.InvalidCampaignCode) {
        setCampaignError(t('routers.edition.purchase.invalidCampaignCode'));
      }
      setPreview(null);
    }
  }, [selected, region, campaignCode, t]);

  useEffect(() => {
    if (plansLoading || !selected) return;
    if (previewTimer.current) clearTimeout(previewTimer.current);
    previewTimer.current = setTimeout(() => {
      void fetchPreview();
    }, 500);
    return () => {
      if (previewTimer.current) clearTimeout(previewTimer.current);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected, region, campaignCode, plansLoading]);

  // 4) handlePay：非预览下单。InvalidOperation → 一户一台门，展示链接而非 toast。
  const handlePay = useCallback(async () => {
    if (!selected) return;
    setSubmitting(true);
    try {
      const request = {
        preview: false as const,
        plan: selected.pid,
        region: region || undefined,
        campaignCode: campaignCode || undefined,
        ...(needsShipping ? { shipping: trimmedShipping } : {}),
      };
      const { order, payUrl } = await api.createOrder(request, { autoRedirectToAuth: false });
      setPreview(order);
      setCampaignError('');
      setAlreadyHasRouter(false);
      setPayDialogOpen(true);
      window.location.href = payUrl;
    } catch (err) {
      console.error('[RouterPurchase] Create order failed:', err);
      if (err instanceof ApiError && err.code === ErrorCode.InvalidCampaignCode) {
        setCampaignError(t('routers.edition.purchase.invalidCampaignCode'));
      } else if (err instanceof ApiError && err.code === ErrorCode.InvalidOperation) {
        setAlreadyHasRouter(true);
      } else {
        toast.error(
          err instanceof ApiError
            ? getApiErrorMessage(err.code, t)
            : t('routers.edition.purchase.createOrderFailed'),
        );
      }
    } finally {
      setSubmitting(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected, region, campaignCode, needsShipping, trimmedShipping.name, trimmedShipping.phone, trimmedShipping.address, t]);

  const handlePayDialogClose = useCallback(() => {
    setPayDialogOpen(false);
    router.push('/account/router');
  }, [router]);

  const payAmount = preview?.payAmount ?? selected?.price ?? 0;
  const payDisabled = !isAuthenticated || !selected || (needsShipping && !shippingComplete) || submitting;

  return (
    <>
      {showNavigation && <Header />}

      <div className="container max-w-4xl xl:max-w-6xl mx-auto py-4 px-4 sm:py-8 sm:px-6 lg:px-8 space-y-6 sm:space-y-8">
        <div className="text-center px-4 sm:px-0">
          <h1 className="text-3xl sm:text-4xl font-black text-foreground mb-3 leading-tight">
            {t('routers.edition.purchase.title')}
          </h1>
          <p className="text-lg text-muted-foreground font-medium leading-relaxed max-w-2xl mx-auto">
            {t('routers.edition.purchase.subtitle')}
          </p>
        </div>

        {!plansLoading && !hardwarePlan && !servicePlan && (
          <p className="text-center text-muted-foreground">{t('routers.edition.purchase.noPlans')}</p>
        )}

        <div className="space-y-6 sm:space-y-8 xl:space-y-0 xl:grid xl:grid-cols-12 xl:gap-8">
          {/* 左栏：登录 + 选择卡 + 地区 + 收货信息 */}
          <div className="xl:col-span-7 space-y-6 sm:space-y-8">
            <PurchaseStep1 />

            {selected && (
              <Card>
                <CardHeader className="pb-0">
                  <CardTitle>{t('routers.edition.purchase.stepSelect')}</CardTitle>
                </CardHeader>
                <CardContent className="space-y-3 pt-0">
                  {choice === 'hardware' ? (
                    <>
                      <div className="flex items-baseline justify-between gap-3">
                        <span className="font-bold text-foreground">
                          {t('routers.edition.purchase.hardwareName')}
                        </span>
                        <span className="text-xl font-black text-foreground">
                          {hardwarePlan && formatUsd(hardwarePlan.price)}
                        </span>
                      </div>
                      <p className="text-sm text-muted-foreground">
                        {t('routers.edition.purchase.hardwareDesc')}
                      </p>
                    </>
                  ) : (
                    <>
                      <div className="flex items-baseline justify-between gap-3">
                        <span className="font-bold text-foreground">
                          {t('routers.edition.purchase.serviceName')}
                        </span>
                        <span className="text-xl font-black text-foreground">
                          {servicePlan && formatUsd(servicePlan.price)}
                        </span>
                      </div>
                      <p className="text-sm text-muted-foreground">
                        {t('routers.edition.purchase.serviceDesc')}
                      </p>
                    </>
                  )}
                  {choice === 'hardware' ? (
                    servicePlan && (
                      <button
                        type="button"
                        onClick={() => setChoice('service')}
                        className="text-sm text-primary underline underline-offset-4 hover:text-primary/80"
                      >
                        {t('routers.edition.purchase.switchToService')}
                      </button>
                    )
                  ) : (
                    hardwarePlan && (
                      <button
                        type="button"
                        onClick={() => setChoice('hardware')}
                        className="text-sm text-primary underline underline-offset-4 hover:text-primary/80"
                      >
                        {t('routers.edition.purchase.switchToHardware')}
                      </button>
                    )
                  )}
                </CardContent>
              </Card>
            )}

            {selected && regions.length > 0 && (
              <Card>
                <CardContent className="space-y-2 py-4">
                  <Label>{t('routers.edition.purchase.regionLabel')}</Label>
                  {regions.length === 1 ? (
                    <p className="text-sm text-foreground">{regionLabel(regions[0])}</p>
                  ) : (
                    <Select value={region} onValueChange={setRegion}>
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {regions.map((r) => (
                          <SelectItem key={r} value={r}>
                            {regionLabel(r)}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  )}
                </CardContent>
              </Card>
            )}

            {needsShipping && (
              <Card>
                <CardHeader className="pb-0">
                  <CardTitle>{t('routers.edition.purchase.shippingTitle')}</CardTitle>
                </CardHeader>
                <CardContent className="space-y-4 pt-0">
                  <div className="space-y-2">
                    <Label htmlFor="router-shipping-name">
                      {t('routers.edition.purchase.shippingName')}
                    </Label>
                    <Input
                      id="router-shipping-name"
                      value={shipping.name}
                      onChange={(e) => setShipping((s) => ({ ...s, name: e.target.value }))}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="router-shipping-phone">
                      {t('routers.edition.purchase.shippingPhone')}
                    </Label>
                    <Input
                      id="router-shipping-phone"
                      value={shipping.phone}
                      onChange={(e) => setShipping((s) => ({ ...s, phone: e.target.value }))}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="router-shipping-address">
                      {t('routers.edition.purchase.shippingAddress')}
                    </Label>
                    <Input
                      id="router-shipping-address"
                      value={shipping.address}
                      onChange={(e) => setShipping((s) => ({ ...s, address: e.target.value }))}
                    />
                  </div>
                </CardContent>
              </Card>
            )}
          </div>

          {/* 右栏：确认卡 + 活动码 + 支付按钮 + 一户一台提示 */}
          <div className="xl:col-span-5">
            <div className="xl:sticky xl:top-8 space-y-6">
              <Card>
                <CardHeader className="pb-0">
                  <CardTitle>{t('routers.edition.purchase.stepPay')}</CardTitle>
                </CardHeader>
                <CardContent className="space-y-3 pt-0">
                  {selected && (
                    <div className="flex items-baseline justify-between gap-3 font-semibold text-foreground">
                      <span>
                        {choice === 'hardware'
                          ? t('routers.edition.purchase.lineHardware')
                          : t('routers.edition.purchase.lineService')}
                      </span>
                      <span>{formatUsd(selected.price)}</span>
                    </div>
                  )}
                  {choice === 'hardware' && servicePlan && (
                    <p className="text-sm text-muted-foreground">
                      {t('routers.edition.purchase.renewNote', { price: formatUsd(servicePlan.price) })}
                    </p>
                  )}

                  <div>
                    {!showCampaign && !campaignCode && (
                      <button
                        type="button"
                        onClick={() => setShowCampaign(true)}
                        className="text-sm text-primary underline underline-offset-4 hover:text-primary/80"
                      >
                        {t('routers.edition.purchase.campaignToggle')}
                      </button>
                    )}
                    {(showCampaign || campaignCode) && (
                      <div className="space-y-1 mt-2">
                        <Input
                          placeholder={t('routers.edition.purchase.campaignPlaceholder')}
                          value={campaignCode}
                          onChange={(e) => {
                            setCampaignCode(e.target.value);
                            setCampaignError('');
                          }}
                        />
                        {campaignError && (
                          <p className="text-sm text-destructive">{campaignError}</p>
                        )}
                      </div>
                    )}
                  </div>

                  <div className="flex items-baseline justify-between gap-3 border-t pt-3 text-lg font-bold text-foreground">
                    <span>{t('routers.edition.purchase.payAmount')}</span>
                    <span>{formatUsd(payAmount)}</span>
                  </div>

                  {alreadyHasRouter && (
                    <div className="space-y-1 text-sm text-destructive">
                      <p>{t('routers.edition.purchase.alreadyHasRouter')}</p>
                      <Link href="/account/router" className="underline underline-offset-4">
                        {t('routers.edition.purchase.goMyRouter')}
                      </Link>
                    </div>
                  )}

                  {!isAuthenticated && (
                    <p className="text-sm text-muted-foreground">
                      {t('routers.edition.purchase.loginFirst')}
                    </p>
                  )}

                  <Button
                    size="lg"
                    className="w-full"
                    disabled={payDisabled}
                    onClick={() => {
                      void handlePay();
                    }}
                  >
                    {submitting
                      ? t('routers.edition.purchase.paying')
                      : t('routers.edition.purchase.payButton')}
                  </Button>

                  <p className="text-xs text-muted-foreground text-center">
                    {t('routers.edition.purchase.oneAccountOneRouter')}
                  </p>
                </CardContent>
              </Card>
            </div>
          </div>
        </div>
      </div>

      <Dialog open={payDialogOpen} onOpenChange={() => {}}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('routers.edition.purchase.payResultTitle')}</DialogTitle>
            <DialogDescription>{t('routers.edition.purchase.payResultBody')}</DialogDescription>
          </DialogHeader>
          <DialogFooter className="flex flex-row justify-between">
            <Button
              variant="default"
              onClick={handlePayDialogClose}
              className="bg-green-600 hover:bg-green-700 text-white"
            >
              {t('routers.edition.purchase.paySuccess')}
            </Button>
            <Button
              variant="outline"
              onClick={handlePayDialogClose}
              className="border-red-300 text-red-700 hover:bg-red-50"
            >
              {t('routers.edition.purchase.payFailed')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {showFooter && <Footer />}
    </>
  );
}

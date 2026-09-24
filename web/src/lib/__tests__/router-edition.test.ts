import { describe, it, expect } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import {
  buildInstallCommand,
  formatUsd,
  routerProgressSteps,
  quotaLevel,
  formatBytes,
  EDITION_PRICE_CENTS,
  ROUTER_PRESALE,
  routerOffer,
  isRouterPresale,
  presaleEndsAt,
  formatShipsFrom,
} from '../router-edition';
import type { UserRouterFulfillment } from '../api';

function f(partial: Partial<UserRouterFulfillment>): UserRouterFulfillment {
  return {
    id: 1, orderId: 1, hardwareSku: 'redmi-ax6s', stage: 'paid',
    shippedAt: 0, activatedAt: 0, credentialMinted: false,
    canMintCredential: false, createdAt: 1,
    ...partial,
  };
}

describe('buildInstallCommand', () => {
  it('拼出单引号包裹凭证的安装命令', () => {
    expect(buildInstallCommand('https://kaitu.io', 'k2subs://u:t@k2.52j.me/api/subs'))
      .toBe("wget -qO- https://kaitu.io/i/k2r | sh -s 'k2subs://u:t@k2.52j.me/api/subs'");
  });
  it('去掉 origin 末尾的斜杠', () => {
    expect(buildInstallCommand('https://kaitu.io/', 'x')).toBe("wget -qO- https://kaitu.io/i/k2r | sh -s 'x'");
  });
});

describe('formatUsd', () => {
  it('美分转美元，整数不带小数', () => {
    expect(formatUsd(39900)).toBe('$399');
    expect(formatUsd(29900)).toBe('$299');
  });
  it('非整数保留两位', () => {
    expect(formatUsd(1999)).toBe('$19.99');
  });
});

describe('routerProgressSteps', () => {
  it('成品：paid 时第一步进行中，共 5 步含发货', () => {
    const steps = routerProgressSteps(f({ stage: 'paid' }));
    expect(steps.map((s) => s.key)).toEqual(['paid', 'provisioning', 'ready', 'shipped', 'online']);
    expect(steps[0].state).toBe('current');
    expect(steps[1].state).toBe('todo');
  });
  it('成品：shipped 时前四步完成或进行中', () => {
    const steps = routerProgressSteps(f({ stage: 'shipped' }));
    expect(steps.slice(0, 3).every((s) => s.state === 'done')).toBe(true);
    expect(steps[3].state).toBe('current');
    expect(steps[4].state).toBe('todo');
  });
  it('自备：没有发货步', () => {
    const steps = routerProgressSteps(f({ hardwareSku: '', stage: 'ready' }));
    expect(steps.map((s) => s.key)).toEqual(['paid', 'provisioning', 'ready', 'online']);
    expect(steps[2].state).toBe('current');
  });
  it('online 时全部完成', () => {
    const steps = routerProgressSteps(f({ stage: 'online' }));
    expect(steps.every((s) => s.state === 'done')).toBe(true);
  });
  it('expired 时全部标为 todo 以外的 done 不成立：返回空数组交给页面显示过期', () => {
    expect(routerProgressSteps(f({ stage: 'expired' }))).toEqual([]);
  });
});

describe('quotaLevel', () => {
  it('按 80% / 95% 分档', () => {
    expect(quotaLevel(0, 100)).toBe('normal');
    expect(quotaLevel(79, 100)).toBe('normal');
    expect(quotaLevel(80, 100)).toBe('warn');
    expect(quotaLevel(94, 100)).toBe('warn');
    expect(quotaLevel(95, 100)).toBe('danger');
  });
  it('total 为 0 时视为 normal', () => {
    expect(quotaLevel(10, 0)).toBe('normal');
  });
});

describe('formatBytes', () => {
  it('按 1024 进位', () => {
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(1024)).toBe('1 KB');
    expect(formatBytes(2 * 1024 ** 4)).toBe('2 TB');
    expect(formatBytes(1.5 * 1024 ** 3)).toBe('1.5 GB');
  });
});

describe('EDITION_PRICE_CENTS', () => {
  it('导出首年 / 续费两个价格常量', () => {
    expect(EDITION_PRICE_CENTS.firstYear).toBe(39900);
    expect(EDITION_PRICE_CENTS.renewal).toBe(29900);
  });

  it('与 docs/router-edition-prod-deploy.md 的套餐 SQL 同源', () => {
    const sql = fs.readFileSync(
      path.resolve(__dirname, '../../../../docs/router-edition-prod-deploy.md'),
      'utf8',
    );
    const stdMatch = sql.match(/'router-std-1y'[^\n]*?,\s*(\d+)\s*,\s*\d+\s*,/);
    const svcMatch = sql.match(/'router-svc-1y'[^\n]*?,\s*(\d+)\s*,\s*\d+\s*,/);
    expect(stdMatch).not.toBeNull();
    expect(svcMatch).not.toBeNull();
    expect(Number(stdMatch![1])).toBe(EDITION_PRICE_CENTS.firstYear);
    expect(Number(svcMatch![1])).toBe(EDITION_PRICE_CENTS.renewal);
  });

  it('ROUTER_PRESALE matches the presale UPDATE and the ship date in the prod deploy doc', () => {
    const doc = fs.readFileSync(
      path.resolve(__dirname, '../../../../docs/router-edition-prod-deploy.md'),
      'utf8',
    );
    // UPDATE plans SET price = 35900, origin_price = 39900 WHERE pid = 'router-std-1y'
    const presale = doc.match(/UPDATE plans SET price = (\d+), origin_price = (\d+) WHERE pid = 'router-std-1y'/);
    expect(presale).not.toBeNull();
    expect(Number(presale![1])).toBe(ROUTER_PRESALE.firstYear);
    expect(Number(presale![2])).toBe(EDITION_PRICE_CENTS.firstYear);
    expect(doc).toContain(`发售日 ${ROUTER_PRESALE.shipsFrom}`);
  });
});

describe('routerOffer (presale window)', () => {
  const beforeLaunch = Date.parse('2026-10-01T12:00:00+08:00');
  const launchMinus1s = Date.parse('2026-11-10T23:59:59+08:00');
  const launch = Date.parse('2026-11-11T00:00:00+08:00');

  it('the window closes at 00:00 Asia/Shanghai on the ship date', () => {
    expect(presaleEndsAt()).toBe(launch);
    expect(isRouterPresale(beforeLaunch)).toBe(true);
    expect(isRouterPresale(launchMinus1s)).toBe(true);
    expect(isRouterPresale(launch)).toBe(false);
  });

  it('before launch: presale price with the list price struck through', () => {
    expect(routerOffer(beforeLaunch)).toEqual({
      presale: true,
      firstYear: 35900,
      originFirstYear: 39900,
      renewal: 29900,
      shipsFrom: '2026-11-11',
    });
  });

  it('from launch: list price, no strike-through', () => {
    expect(routerOffer(launch)).toEqual({ presale: false, firstYear: 39900, renewal: 29900, shipsFrom: '2026-11-11' });
  });

  it('formatShipsFrom renders the calendar day without timezone drift', () => {
    expect(formatShipsFrom('2026-11-11', 'zh-CN')).toBe('11月11日');
    expect(formatShipsFrom('2026-11-11', 'en-GB')).toBe('11 November');
  });
});

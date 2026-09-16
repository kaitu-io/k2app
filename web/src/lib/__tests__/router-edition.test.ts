import { describe, it, expect } from 'vitest';
import {
  buildInstallCommand,
  formatUsd,
  routerProgressSteps,
  quotaLevel,
  formatBytes,
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

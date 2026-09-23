/**
 * 货币符号守卫。
 *
 * 2026-09-23：`account/KaituAccountClient.tsx`、`manager/plans`、
 * `manager/license-key-batches` 三处把 `orders.pay_amount` / `plans.price` 当人民币分渲染，
 * 而同一批数值在 `/purchase` 页是按美元渲染的。生产数据给出判决：wordgate 741 笔已付订单
 * 的 `currency` **全部是 USD**，min 590 / max 19900，与 `plans.price` 逐一对应 —— 也就是说
 * 人民币符号那侧才是错的，它已经错了 741 笔订单，只是从没人把两个页面并排看。
 *
 * 金额展示一律走 `formatMinor()`，由它按币种 + locale 决定符号。
 *
 * 守卫的盲区（故意留的，别误以为它管得更宽）：
 *   - 只拦人民币符号。硬编码的 `$`（PurchaseStep2、PayResultDialog）本次不拦 —— 它们
 *     恰好是对的，把它们也收进 formatMinor 是另一件事，不在本次发布范围。
 *   - 不检查量纲。把「元」当「分」传进 formatMinor 它一样照算，这里管不着。
 */
import { describe, it, expect } from 'vitest';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';

const SRC_ROOT = join(__dirname, '..', '..');
// 用 charCode 构造而不写字面量：否则本文件会被自己拓到。
// 这比“把本文件加进排除名单”安全：排除名单会静默地越长越宽。
const CNY_SIGN = String.fromCharCode(0x00a5);    // U+00A5 YEN SIGN
const CNY_SIGN_FW = String.fromCharCode(0xffe5); // U+FFE5 FULLWIDTH YEN SIGN

function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) {
      if (name === 'node_modules' || name === '.next') continue;
      walk(p, out);
    } else if (/\.tsx?$/.test(name)) {
      out.push(p);
    }
  }
  return out;
}

describe('货币符号守卫', () => {
  it('web/src 里没有任何人民币符号字面量', () => {
    const offenders = walk(SRC_ROOT)
      .flatMap((file) =>
        readFileSync(file, 'utf8')
          .split('\n')
          .map((line, i) => ({ file, line: i + 1, text: line }))
          .filter(({ text }) => text.includes(CNY_SIGN) || text.includes(CNY_SIGN_FW)),
      )
      .map(({ file, line, text }) => `${relative(SRC_ROOT, file)}:${line}: ${text.trim()}`);

    expect(
      offenders,
      `金额一律是 USD 最小单位，展示必须走 formatMinor()。命中：\n${offenders.join('\n')}`,
    ).toEqual([]);
  });

  it('守卫自身有效：能抓到植入的人民币符号', () => {
    // 变异验证 —— 证明上面那条不是因为遍历到 0 个文件而恒绿。
    const files = walk(SRC_ROOT);
    expect(files.length).toBeGreaterThan(50);
    const mutated = `<span>${CNY_SIGN}{(order.payAmount / 100).toFixed(2)}</span>`;
    expect(mutated.includes(CNY_SIGN) || mutated.includes(CNY_SIGN_FW)).toBe(true);
  });
});

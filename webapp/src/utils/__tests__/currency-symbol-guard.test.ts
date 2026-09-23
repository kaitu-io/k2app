/**
 * 货币符号守卫（webapp 侧；web 侧有同构的一份，见 web/src/lib/__tests__）。
 *
 * 2026-09-23：`pages/ProHistory.tsx` 把 `orders.pay_amount` 与 `campaignReduceAmount`
 * 当人民币分渲染。生产数据给出判决：wordgate 741 笔已付订单的 `currency` **全部是 USD**
 * （min 590 / max 19900，与 `plans.price` 逐一对应），所以人民币符号那侧才是错的。
 *
 * 金额展示一律走 `utils/pricing.ts` 的 `formatMinor()`。
 *
 * 盲区（故意的）：只拦人民币符号；不拦恰好正确的硬编码 `$`；不检查量纲。
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
      if (name === 'node_modules') continue;
      walk(p, out);
    } else if (/\.tsx?$/.test(name)) {
      out.push(p);
    }
  }
  return out;
}

describe('货币符号守卫', () => {
  it('webapp/src 里没有任何人民币符号字面量', () => {
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

  it('守卫自身有效：遍历到了足够多的文件', () => {
    // 变异验证 —— 证明上面那条不是因为遍历到 0 个文件而恒绿。
    expect(walk(SRC_ROOT).length).toBeGreaterThan(50);
  });
});

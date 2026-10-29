# UAT: NextPay + Router Edition (真实服务端到端)

> 内容移自 `api/CLAUDE.md`（2026-10 精简）。要跑 `scripts/uat/` 或改 UAT 脚本 / `uat_nextpay_live_test.go` 前读。

仓库里**所有** checkout 测试都替换 `createNextpayCheckoutFn`，所以从不接触 NextPay 的真实 HTTP 契约；
线上 `plans` 里 `product=router` 的行数为 0，所以路由器版那 4000 行在常规环境下等于未执行代码。
这两个盲区由 `scripts/uat/` 下的 UAT 覆盖：

```bash
scripts/uat/nextpay-router-env.sh up          # 一次性 MariaDB+Redis + 真实 NextPay + 真实 Center（~45s）
eval "$(scripts/uat/nextpay-router-env.sh env)"
NEXTPAY_UAT=1 go test -count=1 -run TestUAT ./...   # 在 api/ 下：真实 createNextpayCheckout / ensureOrderCheckout
python3 scripts/uat/nextpay_router_e2e.py     # HTTP 层全链路 55 项断言
scripts/uat/nextpay-router-env.sh down
```

- **唯一被模拟的外部动作**是「Stripe 投递 `checkout.session.completed`」（用 NextPay 自己的 stripe webhook
  secret 签名打它真实的 `/webhooks/stripe`）。NextPay 的结算与出站 webhook、Center 的验签与入账全是真的。
- **Stripe 用测试模式**（`sk_test_*`，脚本硬拒 live key），账号与生产 NextPay 同一个
  （`acct_1RjVqKDe0r2BKV9U`），所以 `payment_method` 的行为可以外推到生产。
- `uat_nextpay_live_test.go` **不带 build tag**，每次普通 `go test` 都参与编译（生产签名一改就编译失败），
  没有 `NEXTPAY_UAT=1` 时跳过。它读 Stripe 密钥走配置文件而**不走 viper** —— 本包共享 viper，
  `logic_stripe_test.go` 的 cleanup 会把 `stripe.secret_key` 置空，依赖 viper 会单跑绿、全量红。
- 环境脚本会**掐断所有对外出口**（Slack webhooks / bot token 清空、`mail.dev_mode`、删掉 `aws` 段防止
  AWS SDK 默认凭证链摸到 `~/.aws/credentials`）。改脚本时不要放宽这几条。
- **`up` 会就地改写 `center/config.yml`**（`center` 是指向 `api` 的符号链接，所以在主仓跑 `up` 改的
  就是你本机那份带真实 slack webhooks / aws 凭证的开发配置，且它 gitignored、git 恢复不了）。
  `render_uat_configs.py` 会先存一份 `center/config.yml.pre-uat.bak`，`down` 负责还原并删除备份——
  **所以跑完必须 `down`**，直接 `pkill` 会把 UAT 版配置永久留在原地。2026-09-23 在主仓跑 `up`
  真的毁过一次（靠生产 `/data/app-configs/kaitu/config.yml` 重建）。

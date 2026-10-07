# 客服 AI 知识库

每个品牌一个目录、一个独立的 OpenAI vector store，互不共用（`TestChatBrandAI_EveryAIBrandIsWired` 守着）。

| 品牌 | 目录 | 语言 | 线上配置 |
|---|---|---|---|
| 开途 | `kaitu/` | 中文 | `openai.filesearch.stores.crm` |
| Overleap | `overleap/` | English | `openai.filesearch.stores.overleap` |

- 只有 `NN-*.md` 会被上传；本 README 不会。
- 系统提示词不在这里：`api/data/system_prompt.md`（开途）、`api/data/system_prompt_overleap.md`（Overleap），随 api 二进制发布。
- **改完文档必须手动同步**，否则机器人看不到：
  `OPENAI_API_KEY=sk-... scripts/sync-kb.sh <brand> <vector_store_id>`
- 品牌隔离：Overleap 文档不得出现中文或另一品牌名；开途文档用户可见文字只写「开途」（`TestChatBrandAI_KnowledgeBaseNoCrossBrand`）。
- 价格、菜单路径、系统要求等事实以代码和线上数据为准，改版时一起改这里。

## Stripe withdrawal procedure (support)

内部流程，不上传给机器人。只适用于 Overleap 由我们通过 Stripe 收费的订阅；App Store 购买由 Apple 退款，我们无法处理。依据：条款 §7（14 天折算撤回：新订阅首付、年付续费；月付续费不在内）、设计 `docs/superpowers/specs/2026-10-07-overleap-payment-phase-a-design.md` §3。

1. **记下通知时刻**：用户提出撤回的消息时间（邮件 / 聊天的发送时间）就是 `notice_at`。窗口和已用天数都按它算，不按处理时刻——所以不要等到处理时才填当前时间。
2. **报价**：`quote_stripe_withdrawal`（`notice_at` 必填）看计划：每条的金额、已用 / 总天数、是否全额（无同意记录时全额）、不合格原因。不合格就按原因回复用户。
3. **回邮件确认金额**：把报价金额回给用户。
4. **执行**：`withdraw_stripe_subscription`，**在用户通知后 14 天内完成**。它会退款、收回会员、取消 Stripe 订阅；通知后又续费扣的款会全额退。中途失败就带 `request_id` 重跑（续跑，不会重复退）。
5. **不要在 Stripe 后台手工部分退款**——执行器按 metadata 查重，手工退款它认不出来，会造成重复退或会员不收回。
6. **我们主动终止**（条款 8.3：长期不活跃 / 停止服务）：用 `mode=termination`，**直接**跑执行器结束服务，不要先用别的方式停掉账号再补跑——`notice_at` 必须在 48 小时内，超时执行器会拒绝原时刻，改用当前时刻又会多收用户没享受服务的天数。
7. **拒付跳过的退款**：执行器发现 charge 处于拒付中会跳过退款（该行 `RefundNote` 记了拒付）。`charge.dispute.closed` 告警到达后，若结案判我们胜、或只是询问，按已存的金额在用户通知后 14 天内**人工补退**（条款 7.4），并把补退的退款 id 写进该行 `RefundNote`。
8. **卡死的请求**（例如 Stripe 持续拒绝退款）：`abandon_stripe_withdrawal` 作废。它会先把已发出的退款落库、必要时补做收回与取消，再把行置 `abandoned`；之后人工处理或重新发起（重新发起不会重复退）。

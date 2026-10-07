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

# 内容登记表

收益统计（`content-roi-report` skill）靠这张表把漏斗里的分组键对回文章。设计：`docs/superpowers/specs/2026-11-10-content-roi-tracking-design.md`。

## 规则

- **站内文章**（`web/content/zh-CN/guides/*.md`）不必登记：文件本身就是清单，身份是进站路径 `/guides/<slug>`，各语言版本自动合并。**slug 发布后不改**；万一改了，在下面「站内别名」登记。
- **站外文章**发布当天在下面「站外文章」加一行。身份 = `<平台>/<话题>`，漏斗 `content` 分组里出现的就是这个键。
- 站外文章里所有指向官网的链接都写成：

  ```
  https://www.kaitu.io/<页面>?utm_source=<平台>&utm_medium=article&utm_campaign=<话题>
  ```

- **平台**只能取下面的值（后端白名单 `api/logic_funnel_compute.go` `funnelContentPlatforms`，两处一起改）：

  | 平台 | utm_source |
  |---|---|
  | 知乎 | `zhihu` |
  | 小红书 | `xiaohongshu` |
  | V2EX | `v2ex` |
  | 微博 | `weibo` |
  | 微信公众号 | `wechat` |
  | X | `x` |
  | Reddit | `reddit` |
  | 其它 | `other` |

- **话题**：小写字母、数字、连字符。对应某篇站内文章时直接用它的 slug；同一平台上的同一话题发第二篇时加后缀（`claude-ban-2`）。话题一经发布不改。
- 不带平台标记的站外流量统计不到：会被算成直接访问或搜索。

## 站外文章

| 键（平台/话题） | 标题 | 地址 | 发布日 | 对应站内 slug | 备注 |
|---|---|---|---|---|---|

## 站内别名

改过 slug 的文章：报告把旧路径与新路径合并成一行。

| 旧路径 | 新路径 | 改动日 |
|---|---|---|

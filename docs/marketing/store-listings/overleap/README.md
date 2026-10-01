# Overleap 商店多语言文案（2026-10-01，随 0.4.12 提审）

- `source.en.json` — 英文源文案（App Store en-US 当时的线上版本拆成字段）。
- `listing.json` — 78 个语言键的译文：`name` / `subtitle` / `keywords`（仅 App Store）/ `promo` / `playShort` / `body` / `iosSubscription` / `linkLabels`。
- `play-extra.json` — Google Play 专用段落（"需要订阅、应用内无内购"声明、注销账号与支持的标签）。
- `BRIEF.md` — 翻译约束（字符上限、禁用词、不得提及其他平台）。

拼装规则：
- App Store 描述 = `body` + `iosSubscription` + 隐私/条款链接；已写入 ASC 50 个语言区。
- Google Play 完整说明 = `body` + `play-extra` 订阅声明 + 隐私/条款/注销/支持链接；已写入 Play 86 个语言（en-* 变体沿用 en-GB 原文）。

译文为机器翻译、未经母语者校对；小语种（rm、eu、is、zu、am、km、lo、my、or、si 等）质量把握最低。
截图未本地化，各语言回退到默认语言的截图。

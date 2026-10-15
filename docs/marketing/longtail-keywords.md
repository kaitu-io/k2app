# 长尾关键词记录（开途 · 境外系统国内访问专题）

只用免费方法：Bing 联想（`api.bing.com/osjson.aspx?mkt=zh-CN`）和 Google 联想（`suggestqueries.google.com`，`hl=en`）。没有搜索量数据，**联想词出现 = 有人在搜**，数量只表示问法的多少，不代表搜索量。策略见 growth 仓库 `strategy/longtail-seo-2026-10-15.md`，写作规则见 `kaitu-access-guides` skill。

## B 组：国际学校 / 留学家庭（2026-10-15 采集）

### 方法
- 中文：每个系统分别加上 `""`、` 国内`、` 登录不了`、` 打不开`、` 很慢`、` 家长端`、` 中国`、` app下载`、` 登录`，各查一次 Bing zh-CN 联想，结果去重。
- 英文：每个系统分别加上 ` china`、` in china`、` not working china`、` vpn`，各查一次 Google 联想。
- **只统计相关的联想词**：去掉同名的其他东西（Canvas ≈ Canva / canvas.dll / 同济；Firefly ≈ Adobe Firefly；UCAS ≈ 中国科学院大学；SAO ≈ 刀剑神域 / 学习通；Common App ≈ common_apps 文件夹），也去掉日文、繁体和与访问无关的问法（价格、备考、考点）。
- **排序规则（skill 规定）**：有中文的“访问 / 中国”类联想词 > 只有英文联想词 > 没有联想词。同一档里，按相关中文联想词数、英文“在中国能不能用”类联想词数排序。
- 补充信号：查“X 打不开”“X 国内”时，联想里出现了该系统的中国专用域名（managebac.cn、toddle.cn），说明有人在找国内入口，作为加分项。

### 结果

| 排名 | 系统 | 档 | 相关中文联想词 | 英文“中国”类联想词 | 代表性问法 | 备注 |
|---|---|---|---|---|---|---|
| 1 | ManageBac | 中文访问类 | 8 | 2 | managebac登录入口 / managebac网页版登录 / managebac学生登录入口；“managebac 打不开”→ managebac.cn、mcc.managebac.cn | 查“打不开”“国内”时联想直接给出 .cn 域名（共 20 条） |
| 2 | College Board | 中文访问类 | 7 | 4 | college board打不开 / college board登录不进去 / college board官网中国；EN: is college board blocked in china / can you access college board in china | 痛点词最多 |
| 3 | Flywire | 中文访问类 | 8 | 1 | flywire 国内 / flywire 中国 / flywire中国官网登录 / 飞汇flywire中国官网；EN: does flywire work in china | 另有 5 条付款流程类（客服电话、交学费外币）没计入 |
| 4 | Toddle | 中文访问类 | 2 | 5 | toddle中国官网入口 / toddle登录；“toddle 国内”→ toddle.cn；EN: toddle china login / toddle china app / toddle china student / toddle family china | 英文的中国类问法在 B 组里最多 |
| 5 | Schoology | 中文仅登录类 | 8 | 3 | schoology官网家长登录 / schoology家长登录；EN: does schoology work in china / schoology vpn / schoology sas china | 中文只有登录入口类，没有“国内 / 打不开” |
| 6 | PowerSchool | 中文仅登录类 | 7 | 1 | powerschool家长登录 / powerschool maple leaf家长登录；EN: powerschool sas china | maple leaf 是国内一所国际学校集团的实例 |
| 7 | Google Classroom | 中文仅登录类 | 6（简体） | 5 | google classroom 网页版 / 电脑版；EN: is google classroom banned in china / how to access google classroom in china | 另有约 10 条繁体、9 条日文没计入。Google 服务在大陆被封的证据充分，适合放进第二批 |
| 8 | Canvas | 有歧义 | 难以区分 | 2 | canvas在国内能用吗 / canvas打不开 / canvas登录失败（混有 Canva、游戏）；EN: accessing canvas in china / using canvas in china | 大学用得多，B 组（K12）的需求要另外确认 |
| 9 | SSAT | 中文非访问类 | 12（考点 / 官网） | 1 | ssat中国官网入口 / ssat考试在中国大陆考点 | 搜索意图是“去哪考”，不是访问问题 |
| 10 | ISEE | 中文非访问类 | 3 | 1 | isee 中国 / isee考试中国官网 | 同上 |
| 11 | Duolingo English Test | 中文非访问类 | 3（简体） | 4 | duolingo english test 费用 / 下载；EN: duolingo english test close vpn or firewall software | ⚠️ 官方考试要求关闭 VPN，单页**不能**推荐加速工具 |
| 12 | Seesaw | 中文仅登录类 | 2 | 0 | seesaw app下载 / seesaw登录 | 英文的 china 联想全是跷跷板 |
| 13 | ClassDojo | 中文仅登录类 | 2 | 1 | classdojo网页版登录入口 / classdojo 下载 | |
| 14 | Common App | 中文仅登录类 | 1 | 0 | commonapp登录入口 | 英文联想被泛义的“common apps”淹没 |
| 15 | UKiset | 中文非访问类 | 2 | 0 | ukiset官网 / ukiset考试 | |
| 16 | UCAS | 有歧义 | 0（全是中科院） | 1 | EN: ucas china application portal | |
| — | Veracross / iSAMS / Gateway to Prep Schools | 无联想 | 0 | 0 | — | 暂不写 |
| — | SAO（学生服务办公室） | 有歧义 | 0 | 0 | 联想全是刀剑神域、学习通 | 暂不写 |

### 第一批（2026-10-15）
汇总页 `guides/intl-school-systems-china-access`，加上排名前 4 的单页：
- `guides/managebac-china-access`：主关键词“ManageBac 登录入口 / 打不开”
- `guides/college-board-china-access`：主关键词“College Board 打不开 / 登录不进去”
- `guides/flywire-china-access`：主关键词“Flywire 国内 / 中国官网登录”
- `guides/toddle-china-access`：主关键词“Toddle 中国官网入口 / 登录”

第二批候选：Schoology、PowerSchool、Google Classroom。

### 复查
- 上线 60 天后用 Search Console 看实际展示的词，和本表对比。
- 联想词会变。下次采集时把日期和结果记在本节下面，不要覆盖旧结果。

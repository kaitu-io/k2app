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

## 搜索量：Bing Webmaster Keyword Research（2026-10-15 采集）

kaitu.io 在 2026-10-15 通过了 Bing Webmaster 验证，从这天起可以用它的 Keyword Research。采集设置：国家选“People's Republic of China”，语言和设备都选 All，时间范围 3 个月（2026-07-15 至 10-12）。表里的数字是 Bing 在中国的**展示次数**，可以近似当作 Bing 上的搜索量。Bing 在国内只占一部分搜索份额，所以这些数字适合用来**比较相对大小**，不能当作全网的总搜索量。

### B 组（国际学校 / 留学）

| 排名 | 系统 | 主词（3 个月） | 访问意图更明确的词 | 备注 |
|---|---|---|---|---|
| 1 | College Board | college board 24.2K | collegeboard 4.2K / ap classroom 5K / bluebook 1.8K / college board login 178 | ✅ 第一批已写 |
| 2 | ManageBac | managebac 12.2K | managebac login 1.3K / www.managebac.com login 119 / managebac官网 42 | ✅ 第一批已写 |
| 3 | ClassDojo | classdojo 10.1K | classdojo login 418 | 中文联想词很少，但 Bing 搜索量高；登录页有中文界面 |
| 4 | Common App | common app 9.3K | commonapp 1.6K / common app login 186 | 中文联想词被“common_apps 文件夹”淹没，Bing 精确词确认有真实需求 |
| 5 | Google Classroom | google classroom 7.8K | google classroom login 72 | Google 服务被封的证据最充分 |
| 6 | Duolingo English Test | duolingo english test 5.2K | …login 19 | ⚠️ 考试要求关闭 VPN，写法要和 College Board 一样小心 |
| 7 | Schoology | schoology 4.6K | schoology login 2.3K | 登录类搜索占一半，访问意图最强 |
| 8 | Flywire | flywire 4.4K | 飞汇 2.4K | ✅ 第一批已写 |
| 9 | Toddle | toddle 4.3K（混有英文单词 toddle） | toddle china 686 / toddle.cn 437 / toddle student 355 / toddle login 195 | ✅ 第一批已写；“toddle china”和“toddle.cn”说明国内用户在找中国版 |
| 10 | Seesaw | seesaw 4K（混有“跷跷板”） | seesaw login 636 / seesaw app 127 | |
| 11 | PowerSchool | powerschool 2K | powerschool login 437 | |
| 12 | SSAT | ssat 625 | — | 搜索意图是考试，不是访问问题 |
| 13 | iSAMS | isams 419 | — | |
| — | UCAS | ucas 25.4K | ucas.com 193 / ucas application 125 / ucas uk 58 | 主词 25.4K 几乎都是中国科学院大学（相关词里“中国科学院大学”有 91.2K），英国 UCAS 只有几百 |
| — | ISEE | isee 1.7K | isee test 17 | 主词几乎都是“isee 图片专家”和浙大信电学院，考试本身的需求很小 |
| — | Canvas | canvas 104.9K | canvas student 460 / canvas login 266 / canvas instructure 82 | 主词混有 Canva（canvas官网 2.4K 多半是 Canva），学习系统的需求在几百量级，而且多是大学 |
| — | Veracross / Gateway to Prep Schools / UKiset | 69 / 34 / 数据不足 | — | 暂不写 |

### A 组（网课平台，供参考）

| 系统 | 主词（3 个月） | 中文联想里的访问类问法 |
|---|---|---|
| Coursera | 37.8K | coursera国内能用吗 / coursera中国能用吗 / coursera为什么打不开 / coursera 中国地区不能用吗 |
| Khan Academy | 15.6K | khan academy打不开 / khan academy为什么打不开 / khan academy kids 下载；EN: is khan academy blocked in china |
| IXL | 15.4K | ixl登录入口 / ixl官网登录入口；EN: is ixl banned in china |
| Quizlet | 14.4K | quizlet国内能用吗 / quizlet打不开 / quizlet注册不了 / quizlet国内平替软件 |
| Google Meet | 7.8K | google meet 国内可以用吗 / google meet 中国 |
| Outschool | 1.1K | outschool官网入口登录 |
| Raz-Kids | 912 | raz kids中国官网 / raz kids安卓 app 下载 |
| BrainPOP | 494 | — |
| Epic! | epic books 229 | 主词被 Epic Games 淹没 |
| Zoom | 167.7K | 太泛，不单独写 |

### 第二批（2026-10-15 选定）
B 组按搜索量排序，跳过已写的 4 个，再跳过 Duolingo English Test（考试要求关闭 VPN，等第三批和其他考试类一起写）。A 组挑一个和家庭场景最相关的：
- `guides/classdojo-china-access`：ClassDojo 10.1K
- `guides/common-app-china-access`：Common App 9.3K + commonapp 1.6K
- `guides/google-classroom-china-access`：Google Classroom 7.8K
- `guides/schoology-china-access`：Schoology 4.6K（其中 login 2.3K）
- `guides/khan-academy-china-access`（A 组）：Khan Academy 15.6K。Coursera（37.8K）的搜索量更大，但读者主要是成人和大学生，和汇总页面向的家庭读者不太一致，留到 A 组单独做汇总时再写。

第三批候选：Duolingo English Test、Seesaw、PowerSchool；A 组 Coursera、Quizlet、IXL。

### 复查
- 上线 60 天后用 Search Console 看实际展示的词，和本表对比。
- 联想词会变。下次采集时把日期和结果记在本节下面，不要覆盖旧结果。

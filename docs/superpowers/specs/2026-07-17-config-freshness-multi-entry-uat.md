# UAT — config.js/ui.js 新鲜度标记 + 多 entry failover

关联方案：`docs/superpowers/specs/2026-07-17-config-freshness-multi-entry-design.md`
合并：`main` merge `5f6f00e0`（分支 `fix/disable-antiblock-relay`）
发布：`Publish Antiblock Config` workflow run `29562660064`（2026-07-17T07:18:22Z，cursor 81）

---

## 0. 前置：CDN 发布态（已机器验证 ✅）

| 项 | 期望 | 结果 |
|---|---|---|
| `ui-theme@dist/ui.js` 存在 | `window.__k2ac={v:1,data:…}` | ✅ |
| `ui-theme@dist/config.js` 存在（旧 app 兼容） | 同上 | ✅ |
| 两文件 `ts` 相同 | 单次计算喂两文件 | ✅ `1784272702` |
| 明文含整数 `ts` | 在 AES-GCM 认证载荷内，镜像无法伪造 | ✅ |
| `entries` | 生成端默认 | ✅ `["https://d1l0lk9fcyd6r8.cloudfront.net","https://k2.52j.me"]` |
| `v/81.js` seed 通道 | append-only | ✅ |

复核命令：

```bash
KEY='9e3573184d5e5b3034a087c33fa2cdb76bd0126238ed08f54d1de8c6ae0eb4ba'
DATA=$(curl -fsS "https://raw.githubusercontent.com/kaitu-io/ui-theme/dist/ui.js" | sed -E 's/.*"data":"([^"]+)".*/\1/')
node -e 'const c=require("crypto");const r=Buffer.from(process.argv[1],"base64");
const d=c.createDecipheriv("aes-256-gcm",Buffer.from(process.argv[2],"hex"),r.slice(0,12));
d.setAuthTag(r.slice(r.length-16));
console.log(JSON.parse(Buffer.concat([d.update(r.slice(12,r.length-16)),d.final()]).toString()))' "$DATA" "$KEY"
```

> ⚠️ **entries 顺序警告**：默认首位是 CloudFront 域（CN 疑似 GFW 封）。GET 并发竞速无感；**登录 POST 顺序**会先在 CloudFront 耗一个 ~4s 超时才切 `k2.52j.me`。UAT 场景 C/D 必须实测这条延迟。若要 CN-first：重跑 workflow 传 `entries` 输入
> `["https://k2.52j.me","https://d1l0lk9fcyd6r8.cloudfront.net"]`。

---

## 1. 冷启动解析（新装 / 清缓存）

**目的**：验证新 app 拉 `ui.js`（不再是 `config.js`）、解出 `{entries,ts}`、原子写入单一记录 `k2_entry_cfg`。

步骤（桌面 `make dev-standalone` 或真机装新构建）：
1. 清空存储：DevTools → Application → Local Storage → 删除 `k2_entry_cfg` 与遗留 `k2_entry_url`。
2. 冷启动 app / 刷新。
3. 触发任一 Center API 调用（登录页加载即会走 `resolveAndFetch`）。

**期望**：
- Network 面板出现对 `…@dist/ui.js` 的请求（**不应**再请求 `config.js`）。
- `localStorage['k2_entry_cfg']` = `{"entries":["https://d1l0lk9fcyd6r8.cloudfront.net","https://k2.52j.me"],"ts":1784272702}`。
- **无** `k2_entry_url` 被写回（旧 poisoned 键已不迁移）。

控制台快速断言（standalone）：
```js
JSON.parse(localStorage.getItem('k2_entry_cfg'))   // → {entries:[…], ts:1784272702}
localStorage.getItem('k2_entry_url')               // → null
```

---

## 2. ts 后台升级（陈旧镜像盖不掉新缓存）

**目的**：验证 `commitIfFresher` 的 ts 门控——旧 ts 的镜像响应在后台刷新时**不覆盖**更新的缓存。

步骤：
1. 手动把缓存改旧：`localStorage.setItem('k2_entry_cfg', JSON.stringify({entries:['https://k2.52j.me'], ts: 1})); location.reload()`。
2. 观察一次 API 调用后的后台刷新（cache hit → `refreshEntryInBackground`）。

**期望**：后台从 CDN 拿到 `ts=1784272702` 的载荷 → `1784272702 >= 1` → 覆盖，`k2_entry_cfg.ts` 变为 `1784272702`，`entries` 变为 2 条。反向（把缓存改成 `ts: 9999999999`）则**不应**被 CDN 的旧 ts 覆盖。

> 竞态不变量：`commitIfFresher` 全同步、无 `await`——多镜像 `.then` 微任务间不会读改写撕裂。此项靠单测覆盖（`antiblock.test.ts` ts-freshness describe），UAT 只做端到端确认。

---

## 3. 多 entry 竞速 —— GET（幂等，并发）

**目的**：某个 entry 不可达时，GET 竞速到可达 entry，用户无感。

步骤（需能屏蔽一个域，如改 hosts 把 CloudFront 指到黑洞，或断其网络）：
1. 令 `d1l0lk9fcyd6r8.cloudfront.net` 不可达（hosts → `127.0.0.1` 或防火墙 drop）。
2. 触发一个 GET 型 Center 调用（如账号信息刷新）。

**期望**：请求成功，走 `k2.52j.me`；两个 fetch 并发发出，CloudFront 那条超时/连接失败被丢弃，`k2.52j.me` 首个 HTTP 响应胜出。**无明显延迟**（并发，非串行等待）。

---

## 4. 多 entry failover —— POST（非幂等，顺序，绝不重放）

**目的**：登录等 POST 只在**连接级失败**（证明请求未到后端）才切下一 entry；拿到任何 HTTP 响应（含 4xx/5xx）即返回，绝不重放。

### 4a. 连接失败 → failover
1. 令 CloudFront 连接级不可达（drop SYN）。
2. 执行登录（POST `/api/auth/login`）。

**期望**：CloudFront `fetchOnce` 返回 `null`（连接失败）→ 顺序切到 `k2.52j.me` → 登录成功。观测到 ~≤4s 的首 entry 超时延迟（见 §0 警告）。

### 4b. HTTP 响应（如 401/500）→ 不 failover、不重放
1. CloudFront **可达**但让后端对该请求返回非 2xx（或直接用正常网络，首 entry 就是源站）。
2. 执行一次会返回 4xx/5xx 的 POST。

**期望**：拿到 HTTP 响应即返回该状态，**不**再向 `k2.52j.me` 重发同一 POST（避免重复下单/重复登录副作用）。Network 面板中该 POST **只出现一次到达后端**。

---

## 5. 旧 app 向后兼容

**目的**：已发布的旧构建仍读 `config.js`，不受 `ui.js` 迁移影响。

步骤：用一个 merge 前的旧构建（或旧 webapp bundle）冷启动。

**期望**：请求 `…@dist/config.js`（旧代码路径），解出的 entries 正常工作。新旧 app 并存期两文件都在 CDN。

---

## 6. 全兜底（所有源不可达）

**目的**：CDN 全挂时回落 `DEFAULT_ENTRY`。

步骤：断网 / 屏蔽所有 10 个镜像域后冷启动（无缓存）。

**期望**：`resolveEntries()` 兜底返回 `['https://k2.52j.me']`，app 仍尝试直连控制面主机；不崩、不抛。

---

## 判定

- §0–§2、§5、§6：桌面/standalone 即可验证。
- §3、§4：需能屏蔽单个域的网络环境，**failover 延迟与"不重放"是本次改动的核心增量，必测**。
- **CN 真机**：§4a 的 CloudFront-first 延迟只有在真实 GFW 环境才有代表性；若延迟不可接受，重跑 workflow 改 `entries` 顺序为 CN-first。

发布信心：代码 + 单测 10/10；**业务信心待本 UAT §3/§4/§CN 通过后方可上调**（无真机 failover smoke 前按 feedback_release_confidence_framework 封顶 6–7）。

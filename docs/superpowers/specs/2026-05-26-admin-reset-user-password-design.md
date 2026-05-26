---
title: Admin Reset User Password
date: 2026-05-26
status: draft
owner: david
---

# Admin Reset User Password — Design

## 1. 目标

让超级管理员（`IsAdmin=true`）能在用户详情页直接为指定用户设置一个新密码，主要用于：

- 用户账号因连续失败被锁、邮箱失联无法走自助重置；
- 客服远程协助场景需要即时下发临时密码。

形态：**管理员直接输入明文新密码**（在受限内网管理后台的 Dialog 中），后端 hash 入库；不走"发邮件让用户自己改"链路。

## 2. 非目标

- ❌ 不支持自助"忘记密码"邮件链路（后续可独立做，与本 spec 不耦合）。
- ❌ 不强制踢出现有设备 token / web session（与现有 `api_set_password` 行为对齐）。
- ❌ 不引入双人审批（与改邮箱 / 角色 / tier 同档，依赖 audit log 兜底）。
- ❌ 不向 support 角色开放（仅 `IsAdmin=true` 可用）。

## 3. 现状参照

| 现成机制 | 文件 / 位置 | 在本 spec 中的复用方式 |
|---|---|---|
| 密码强度门限 (`zxcvbn ≥ 3`、`len ≥ 10`、penalize 邮箱) | `api/logic_password.go` `ValidatePasswordStrength` | 直接调用，零改动 |
| 密码 hash | `api/logic_password.go` `UserPasswordHash` | 直接调用 |
| Lock / 失败计数清零（修改密码自动） | `api/api_user.go` `api_set_password` 第 415–417 行 | 在 admin 路径同样清空 |
| 改密成功后邮件通知 | `api/logic_email.go` `passwordChangedTemplate` + `emailToUser` | 新增 `adminResetPasswordTemplate`（措辞区分），fire-and-log |
| Admin 端点路由模式 | `api/route.go` `admin := r.Group("/app") ... AdminRequired()` | 新增 `POST /app/users/:uuid/password` |
| Admin 用户管理 handler 模式 | `api/api_admin_user.go` `api_admin_update_user_email` | 同形态：取 uuid → 校验 → 写 → audit |
| Audit log | `api/api_admin_*.go` `WriteAuditLog(c, action, entity, id, meta)` | action `user_admin_reset_password`；meta **不含密码明文/hash**，含 reason |
| Dialog UI 模板（带 reason） | `web/.../detail/page.tsx` "修改档位对话框"（line 1521+） | 复制结构 |
| 高危操作菜单 | `web/.../detail/components/MoreActionsMenu.tsx` | 新增一项"重置密码" |

## 4. 后端 API

### 4.1 路由

新增到 `api/route.go` 的 `admin` group（279 行附近，挨着 `admin.PUT("/users/:uuid/email", api_admin_update_user_email)`）：

```go
admin.POST("/users/:uuid/password", api_admin_set_user_password)
```

**鉴权**：仅靠 `admin` group 已挂的 `AdminRequired()`。该中间件已检查 `user.IsAdmin == true`（`middleware.go:607`），等价于 superadmin-only。**不挂 `EnforceDeviceClass`**（区别于自助 `api_set_password`，admin 在 web 后台操作无设备绑定上下文）。

### 4.2 Handler

新增到 `api/api_admin_user.go`，紧跟 `api_admin_update_user_email` 后：

```go
// AdminSetUserPasswordRequest is the request body for admin-driven password reset.
type AdminSetUserPasswordRequest struct {
    Password        string `json:"password"        binding:"required"`
    ConfirmPassword string `json:"confirmPassword" binding:"required"`
    Reason          string `json:"reason"          binding:"required"`
}

// api_admin_set_user_password lets a superadmin set a new password for any user.
// Mirrors api_set_password but does not require old password (no user session)
// and writes an audit log + admin-flavored notification email.
func api_admin_set_user_password(c *gin.Context) {
    uuid := c.Param("uuid")

    var req AdminSetUserPasswordRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        Error(c, ErrorInvalidArgument, err.Error())
        return
    }
    if req.Password != req.ConfirmPassword {
        Error(c, ErrorInvalidArgument, "passwords do not match")
        return
    }
    if len(strings.TrimSpace(req.Reason)) < 3 {
        Error(c, ErrorInvalidArgument, "reason too short")
        return
    }

    var user User
    if err := db.Get().Preload("LoginIdentifies").Where(&User{UUID: uuid}).First(&user).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            Error(c, ErrorNotFound, "user not found")
            return
        }
        log.Errorf(c, "find user %s failed: %v", uuid, err)
        Error(c, ErrorSystemError, "find user failed")
        return
    }

    userInputs := collectUserInputsForPasswordStrength(c, &user)
    if errKey := ValidatePasswordStrength(req.Password, userInputs); errKey != "" {
        Error(c, ErrorInvalidArgument, errKey)
        return
    }

    hash, err := UserPasswordHash(req.Password)
    if err != nil {
        log.Errorf(c, "hash password for user %s failed: %v", uuid, err)
        Error(c, ErrorSystemError, "hash password failed")
        return
    }

    user.PasswordHash = hash
    user.PasswordFailedAttempts = 0
    user.PasswordLockedUntil = 0
    if err := db.Get().Save(&user).Error; err != nil {
        log.Errorf(c, "save password for user %s failed: %v", uuid, err)
        Error(c, ErrorSystemError, "save password failed")
        return
    }

    // Audit log — reason recorded; password value never logged.
    WriteAuditLog(c, "user_admin_reset_password", "user", uuid, map[string]string{
        "reason": strings.TrimSpace(req.Reason),
    })

    // Notification — admin-flavored template, fire-and-log.
    meta := AdminResetPasswordMeta{
        ChangeTime: time.Now().Format("2006-01-02 15:04:05"),
        AdminEmail: adminDisplayEmail(c), // best-effort, may be ""
    }
    if err := emailToUser(c, int64(user.ID), adminResetPasswordTemplate, meta); err != nil {
        log.Errorf(c, "send admin-reset notification to user %s failed: %v", uuid, err)
    }

    log.Infof(c, "superadmin reset password for user %s; reason=%q", uuid, req.Reason)
    SuccessEmpty(c)
}
```

**保留密码明文于内存生命周期**：仅停留在 `req.Password` 局部变量直到 hash 完成，函数返回即出栈。**绝不**写入 audit meta / log message / 错误体。

**`adminDisplayEmail(c)` helper**：拿当前 admin 的解密邮箱用于邮件正文，可放到 `api_admin_user.go` 或 `logic_email.go`，best-effort（解密失败返回 `""`，邮件模板能渲染 `(未知)` 兜底）。

### 4.3 邮件模板

新增到 `api/logic_email.go`：

```go
type AdminResetPasswordMeta struct {
    ChangeTime string
    AdminEmail string // 可能为空字符串
}

adminResetPasswordTemplate = EmailTemplate[AdminResetPasswordMeta]{
    Subject: "Kaitu 账号密码已被管理员重置",
    Body: `尊敬的用户：

您的 Kaitu 账号密码刚刚被管理员重置。

详细信息：
- 操作时间：{{.ChangeTime}}
- 操作人：{{if .AdminEmail}}{{.AdminEmail}}{{else}}（系统管理员）{{end}}

如果您不知情，或这并非您主动联系客服请求的操作，请立即联系 support@kaitu.io。

此致
系统通知`,
}
```

模板与 `passwordChangedTemplate` 文案明确区分，便于用户判断"是我自己改的还是 admin 改的"。

### 4.4 错误码映射

| 场景 | 错误码 | message |
|---|---|---|
| Body 缺字段 / JSON 不合法 | `ErrorInvalidArgument` | `c.ShouldBindJSON` err |
| 两次密码不一致 | `ErrorInvalidArgument` | `passwords do not match` |
| reason 长度 < 3 | `ErrorInvalidArgument` | `reason too short` |
| 用户 uuid 不存在 | `ErrorNotFound` | `user not found` |
| 密码太短 | `ErrorInvalidArgument` | `password_too_short` |
| zxcvbn 太弱 | `ErrorInvalidArgument` | `password_too_weak` |
| hash / DB / 邮件解密 等内部故障 | `ErrorSystemError` | `<具体动作> failed` |

所有 message 为内部 debug 用，前端按 `web/src/lib/api-errors.ts` 的 `getApiErrorMessageZh(code, message)` 映射。`password_too_short` / `password_too_weak` **已在 `api-errors.ts:115–116` 覆盖**，无需新增映射。`passwords do not match` / `reason too short` / `user not found` 这几条交由 `getApiErrorMessageZh` 在 message 未命中时 fallback 到通用文案（如「请求参数有误」「资源不存在」）。如发现 fallback 文案不够友好，前端 Dialog 内可对 `password_too_short`/`password_too_weak` 之外的失败显式 toast"重置失败：<原因>"。

## 5. 前端 UI

### 5.1 入口

修改 `web/src/app/(manager)/manager/users/detail/components/MoreActionsMenu.tsx`：

在现有「硬删除用户」`DropdownMenuItem` **之前**插入一项：

```tsx
<DropdownMenuItem
  className="cursor-pointer"
  onClick={() => setShowResetPasswordDialog(true)}
>
  <KeyRound className="mr-2 h-4 w-4" />
  {"重置密码"}
</DropdownMenuItem>
<DropdownMenuSeparator />
```

`KeyRound` 来自 `lucide-react`（已在项目依赖中）。

### 5.2 Dialog 组件

新建 `web/src/app/(manager)/manager/users/detail/components/ResetPasswordDialog.tsx`，结构对齐"修改档位对话框"（`page.tsx:1521+`）：

```tsx
"use client";

import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import { getApiErrorMessageZh } from "@/lib/api-errors";
import { toast } from "sonner";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter,
  DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Button } from "@/components/ui/button";

interface ResetPasswordDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  userUUID: string;
  userEmail: string;
}

export function ResetPasswordDialog({
  open, onOpenChange, userUUID, userEmail,
}: ResetPasswordDialogProps) {
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [reason, setReason] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);

  const reset = () => { setPassword(""); setConfirmPassword(""); setReason(""); };

  const handleSubmit = async () => {
    if (password.length < 10) { toast.error("密码至少 10 位"); return; }
    if (password !== confirmPassword) { toast.error("两次输入不一致"); return; }
    if (reason.trim().length < 3) { toast.error("请填写变更原因（≥3 字符）"); return; }

    setIsSubmitting(true);
    try {
      await api.request(`/app/users/${userUUID}/password`, {
        method: "POST",
        body: JSON.stringify({ password, confirmPassword, reason: reason.trim() }),
      });
      toast.success(`已为 ${userEmail} 重置密码`);
      reset();
      onOpenChange(false);
    } catch (e) {
      if (e instanceof ApiError) {
        toast.error(getApiErrorMessageZh(e.code, e.message));
      } else {
        toast.error("重置密码失败");
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) reset(); onOpenChange(o); }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{"重置用户密码"}</DialogTitle>
          <DialogDescription>
            {`为 ${userEmail} 设置新密码。操作会写入审计日志并发送通知邮件给用户。`}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-4">
          <div className="space-y-2">
            <label className="text-sm font-medium">{"新密码"}<span className="text-red-500 ml-1">*</span></label>
            <Input
              type="password"
              autoComplete="new-password"
              placeholder="至少 10 位，强度需达到 zxcvbn ≥ 3"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
          <div className="space-y-2">
            <label className="text-sm font-medium">{"确认新密码"}<span className="text-red-500 ml-1">*</span></label>
            <Input
              type="password"
              autoComplete="new-password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
            />
          </div>
          <div className="space-y-2">
            <label className="text-sm font-medium">{"变更原因"}<span className="text-red-500 ml-1">*</span></label>
            <Textarea
              placeholder="例如：用户来电请求重置（工单 #1234）。将写入审计日志。"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              rows={3}
            />
          </div>
          <p className="text-xs text-muted-foreground">
            {"提示：现有设备 token / Web 会话不会被强制失效；用户登录所用密码立即更新。"}
          </p>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={isSubmitting}>
            {"取消"}
          </Button>
          <Button onClick={handleSubmit} disabled={isSubmitting}>
            {isSubmitting ? "提交中..." : "确认重置"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
```

### 5.3 MoreActionsMenu 集线

`MoreActionsMenu.tsx` 增加 state：

```tsx
const [showResetPasswordDialog, setShowResetPasswordDialog] = useState(false);
```

底部渲染：

```tsx
<ResetPasswordDialog
  open={showResetPasswordDialog}
  onOpenChange={setShowResetPasswordDialog}
  userUUID={userUUID}
  userEmail={userEmail}
/>
```

**注意 webapp 内禁用 `window.confirm`** 的反馈规则（[[feedback_no_window_confirm_in_webapp]]）虽然主要针对 webapp，但 `web/manager` 也遵从同一惯例——本 spec 全部 Dialog，无 `confirm`。

## 6. 安全 & 合规

| 关注点 | 处理 |
|---|---|
| 密码明文不入日志 | 所有 `log.*` 调用只 reference `uuid` 与 `req.Reason`，never `req.Password` |
| 密码明文不入 audit meta | `WriteAuditLog` meta map 只含 `{reason}` |
| 密码明文不入错误响应 | 错误体只回 `password_too_short` / `password_too_weak` 等 enum |
| HTTPS only | manager 走 kaitu.io 主域；Cookie HttpOnly + CSRF token（已有） |
| Admin 鉴权 | `AdminRequired()` 中间件 = `IsAdmin == true` |
| Reason 强制 | handler 校验 `len(trim) ≥ 3`，前端校验同步 |
| 通知用户 | `adminResetPasswordTemplate` fire-and-log，失败不阻断操作 |
| Operator email 可追溯 | 邮件正文 + audit log 都包含 admin 操作者标识（audit 走 `WriteAuditLog` 现有签名记录的 `ReqUserID`） |

**已知开口**：现有设备 token / web session 不被失效。若用户账号被劫持后 admin 主动 reset，劫持者持有的 token 仍能用直到自然过期。本 spec 不解决——交由独立的"踢出所有设备"功能（不在 scope）。文案在 Dialog 提示框已说明。

## 7. 测试矩阵

放在 `api/api_admin_user_password_test.go`（新文件）。采用 **Mock DB tier**（`SetupMockDB(t)` + `testInitConfig()`），无需 `skipIfNoConfig`。

| # | 场景 | 期望 |
|---|---|---|
| T1 | 缺 password 字段 | code = `ErrorInvalidArgument` |
| T2 | password ≠ confirmPassword | `ErrorInvalidArgument`, message `passwords do not match` |
| T3 | reason 空 / `"  "` / 长度 < 3 | `ErrorInvalidArgument`, message `reason too short` |
| T4 | 用户 uuid 不存在 | `ErrorNotFound` |
| T5 | 密码 < 10 位 | `ErrorInvalidArgument`, message `password_too_short` |
| T6 | 密码 = 用户邮箱本身 / 含 email local part | `ErrorInvalidArgument`, message `password_too_weak` |
| T7 | 合法请求 | code=0；DB 中 `PasswordHash` 已更新，且 `UserPasswordVerify(newPassword, hash)==true`；`PasswordFailedAttempts=0`；`PasswordLockedUntil=0` |
| T8 | 之前账号被锁（`PasswordLockedUntil > now`），合法 reset | 锁字段清零；下次登录不再受锁影响（断言 `IsAccountLocked == false`） |
| T9 | 邮件发送失败 | code=0（fire-and-log），不影响主流程；log 中含 error |
| T10 | 非 admin 调用（`IsAdmin=false`） | `ErrorForbidden`（由 `AdminRequired()` 抛出，不进 handler） |

**E2E 缺口**：manager Dialog 的交互目前没有 Playwright 覆盖（参考 `web/tests/`，多数 Dialog 也没 E2E）。本 spec 不强制新增 E2E，仅在 spec 风险栏标记。

## 8. Audit log 验证

`WriteAuditLog` 在已有 admin 操作中是同步写入的（参考 `api_admin_user.go` `api_admin_update_user_email` 末尾，`api_admin_cloud.go` 系列也同）。本 spec 不引入新机制——但 T7 测试 case 内额外断言审计表插入了一条 `action="user_admin_reset_password"` + `entity_id=<uuid>` + meta 含 `reason` 的记录。

## 9. 实施清单（简表，详细 plan 留给 writing-plans）

1. 后端 handler + 邮件模板 + meta 类型（`api/api_admin_user.go`, `api/logic_email.go`）
2. 路由注册（`api/route.go`）
3. 测试套件 `api/api_admin_user_password_test.go`（10 case，Mock DB）
4. 前端 Dialog `web/.../detail/components/ResetPasswordDialog.tsx`
5. `MoreActionsMenu.tsx` 增加菜单项 + Dialog 集成
6. Verification：`cd api && go test ./... -run TestAdminSetUserPassword` + `cd web && yarn lint && yarn test` + `cd web && yarn build`
7. 手动 smoke：本地启 Center + web，登录 superadmin → 找一个 test 用户 → 重置 → 用新密码 `POST /api/auth/web-login/password` 验证可登录 → 检查收到邮件

## 10. Out of scope

- 自助"忘记密码"邮件链路 / 一次性 token
- "踢出所有设备 / 失效所有 token"
- support 角色的密码协助
- 临时密码（强制下次登录修改）的状态机
- 双因素 / re-auth challenge admin 自身后再放行

这些都可独立做，与本 spec 互不依赖。

## 11. 风险

| # | 风险 | 缓解 |
|---|---|---|
| R1 | Admin 把弱密码强发给用户，被撞库 | `ValidatePasswordStrength` 同等门限；admin 也被拒绝 |
| R2 | Admin 滥用：偷偷改某用户密码登录其账号 | audit log + 通知邮件双重信号；用户可见"被管理员重置" |
| R3 | manager 前端 / Center API 任一侧把明文写日志 | code review 重点扫 `log.*` 与 `WriteAuditLog` 调用点，本 spec 已显式禁止 |
| R4 | 现有设备 token 不失效，劫持后 reset 无用 | 文案说明；后续独立功能解决 |
| R5 | `adminDisplayEmail` 解密失败导致邮件正文显示"（系统管理员）"含糊 | 模板已 fallback；audit log 是权威操作人来源 |

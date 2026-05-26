"use client";

import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import { getApiErrorMessageZh } from "@/lib/api-errors";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
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
  open,
  onOpenChange,
  userUUID,
  userEmail,
}: ResetPasswordDialogProps) {
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [reason, setReason] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);

  const resetForm = () => {
    setPassword("");
    setConfirmPassword("");
    setReason("");
  };

  const handleSubmit = async () => {
    if (password.length < 10) {
      toast.error("密码至少 10 位");
      return;
    }
    if (password !== confirmPassword) {
      toast.error("两次输入的密码不一致");
      return;
    }
    if (reason.trim().length < 3) {
      toast.error("请填写变更原因（≥3 字符）");
      return;
    }

    setIsSubmitting(true);
    try {
      await api.request(`/app/users/${userUUID}/password`, {
        method: "POST",
        body: JSON.stringify({
          password,
          confirmPassword,
          reason: reason.trim(),
        }),
      });
      toast.success(`已为 ${userEmail} 重置密码`);
      resetForm();
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
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) resetForm();
        onOpenChange(o);
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{"重置用户密码"}</DialogTitle>
          <DialogDescription>
            {`为 ${userEmail} 设置新密码。操作会写入审计日志并向用户发送通知邮件。`}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-4">
          <div className="space-y-2">
            <label className="text-sm font-medium">
              {"新密码"}
              <span className="text-red-500 ml-1">{"*"}</span>
            </label>
            <Input
              type="password"
              autoComplete="new-password"
              placeholder="至少 10 位，强度需通过 zxcvbn ≥ 3"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
          <div className="space-y-2">
            <label className="text-sm font-medium">
              {"确认新密码"}
              <span className="text-red-500 ml-1">{"*"}</span>
            </label>
            <Input
              type="password"
              autoComplete="new-password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
            />
          </div>
          <div className="space-y-2">
            <label className="text-sm font-medium">
              {"变更原因"}
              <span className="text-red-500 ml-1">{"*"}</span>
            </label>
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
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={isSubmitting}
          >
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

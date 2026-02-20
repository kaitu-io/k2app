"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { toast } from "sonner";
import { api } from "@/lib/api";

interface EditEmailDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  userUUID: string;
  currentEmail: string;
  onSuccess: () => void;
}

export function EditEmailDialog({
  open,
  onOpenChange,
  userUUID,
  currentEmail,
  onSuccess,
}: EditEmailDialogProps) {
  const [newEmail, setNewEmail] = useState("");
  const [isSaving, setIsSaving] = useState(false);

  const handleSubmit = async () => {
    if (!newEmail.trim()) {
      toast.error("请输入新邮箱地址");
      return;
    }

    // 简单的邮箱格式验证
    const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
    if (!emailRegex.test(newEmail)) {
      toast.error("请输入有效的邮箱地址");
      return;
    }

    if (newEmail.toLowerCase() === currentEmail.toLowerCase()) {
      toast.error("新邮箱与当前邮箱相同");
      return;
    }

    setIsSaving(true);
    try {
      await api.updateUserEmail(userUUID, { email: newEmail.toLowerCase() });
      toast.success("邮箱修改成功");
      setNewEmail("");
      onOpenChange(false);
      onSuccess();
    } catch (error) {
      console.error("Failed to update email:", error);
      if (error instanceof Error) {
        if (error.message.includes("already in use")) {
          toast.error("该邮箱已被其他用户使用");
        } else {
          toast.error(error.message || "修改邮箱失败");
        }
      } else {
        toast.error("修改邮箱失败");
      }
    } finally {
      setIsSaving(false);
    }
  };

  const handleClose = () => {
    setNewEmail("");
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{"修改用户邮箱"}</DialogTitle>
          <DialogDescription>
            {"修改用户的登录邮箱地址。此操作无需用户验证，请谨慎操作。"}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-4">
          <div className="space-y-2">
            <label className="text-sm font-medium">{"当前邮箱"}</label>
            <Input value={currentEmail} disabled className="bg-muted" />
          </div>
          <div className="space-y-2">
            <label className="text-sm font-medium">{"新邮箱"}</label>
            <Input
              type="email"
              placeholder="请输入新邮箱地址"
              value={newEmail}
              onChange={(e) => setNewEmail(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !isSaving) {
                  handleSubmit();
                }
              }}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={handleClose} disabled={isSaving}>
            {"取消"}
          </Button>
          <Button onClick={handleSubmit} disabled={isSaving}>
            {isSaving ? "保存中..." : "确认修改"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

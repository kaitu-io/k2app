"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { MoreHorizontal, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { useRouter } from "next/navigation";

interface MoreActionsMenuProps {
  userUUID: string;
  userEmail: string;
}

export function MoreActionsMenu({ userUUID, userEmail }: MoreActionsMenuProps) {
  const router = useRouter();
  const [showFirstConfirm, setShowFirstConfirm] = useState(false);
  const [showSecondConfirm, setShowSecondConfirm] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);

  const handleDeleteClick = () => {
    setShowFirstConfirm(true);
  };

  const confirmFirstDelete = () => {
    setShowFirstConfirm(false);
    setShowSecondConfirm(true);
  };

  const confirmSecondDelete = async () => {
    if (isDeleting) return;

    setIsDeleting(true);
    try {
      await api.request("/app/users/hard-delete", {
        method: "POST",
        body: JSON.stringify({ userUuids: [userUUID] }),
      });

      setShowSecondConfirm(false);
      toast.success("用户及其所有关联数据已删除");

      // 返回用户列表页
      router.push("/manager/users");
    } catch (error) {
      console.error("Failed to delete user:", error);
      toast.error("删除失败，请重试或联系管理员");
    } finally {
      setIsDeleting(false);
    }
  };

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="sm" className="h-8 w-8 p-0">
            <MoreHorizontal className="h-4 w-4" />
            <span className="sr-only">{"更多操作"}</span>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuSeparator />
          <DropdownMenuItem
            className="text-destructive focus:text-destructive cursor-pointer"
            onClick={handleDeleteClick}
          >
            <Trash2 className="mr-2 h-4 w-4" />
            {"硬删除用户"}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      {/* 第一次确认对话框 */}
      <Dialog open={showFirstConfirm} onOpenChange={setShowFirstConfirm}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{"确认删除用户"}</DialogTitle>
            <DialogDescription>
              {"您即将硬删除用户 "}
              <span className="font-semibold">{userEmail}</span>
              {" 及其所有关联数据（设备、订单、邀请码、钱包等）。此操作不可撤销！"}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setShowFirstConfirm(false)}
            >
              {"取消"}
            </Button>
            <Button variant="destructive" onClick={confirmFirstDelete}>
              {"继续"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 第二次确认对话框 */}
      <Dialog open={showSecondConfirm} onOpenChange={setShowSecondConfirm}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{"最后确认"}</DialogTitle>
            <DialogDescription className="space-y-2">
              <p className="font-semibold text-destructive">
                {"⚠️ 这是最后一次确认！"}
              </p>
              <p>{"您确定要永久删除这个用户吗？所有数据将被彻底清除，包括："}</p>
              <ul className="list-disc list-inside space-y-1">
                <li>{"用户账户信息"}</li>
                <li>{"所有设备记录"}</li>
                <li>{"所有订单记录"}</li>
                <li>{"所有邀请码"}</li>
                <li>{"钱包及交易记录"}</li>
                <li>{"邮件发送记录"}</li>
              </ul>
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setShowSecondConfirm(false)}
              disabled={isDeleting}
            >
              {"取消"}
            </Button>
            <Button
              variant="destructive"
              onClick={confirmSecondDelete}
              disabled={isDeleting}
            >
              {isDeleting ? "删除中..." : "确认删除"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

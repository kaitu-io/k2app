"use client";

export const dynamic = "force-dynamic";

import { useState, useEffect, useCallback } from "react";
import { useParams } from "next/navigation";
import { api, ApiError, ErrorCode, type User, type AddMemberRequest } from "@/lib/api";
import { toast } from "sonner";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Users,
  Plus,
  Trash2,
  RefreshCw,
  Mail,
  Clock,
  AlertCircle,
  Loader2,
  ArrowLeft,
  User as UserIcon,
} from "lucide-react";
import Link from "next/link";

// Email validation helper
function isValidEmail(email: string): boolean {
  const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
  return emailRegex.test(email.trim());
}

// Get member status badge
function getMemberStatusBadge(member: User) {
  if (!member.expiredAt) {
    return (
      <Badge variant="secondary">
        {"未激活"}
      </Badge>
    );
  }

  const expiredAt = new Date(member.expiredAt * 1000);
  const now = new Date();

  if (expiredAt <= now) {
    return (
      <Badge variant="destructive">
        {"已过期"}
      </Badge>
    );
  }

  const daysLeft = Math.ceil((expiredAt.getTime() - now.getTime()) / (1000 * 60 * 60 * 24));
  if (daysLeft <= 7) {
    return (
      <Badge variant="outline" className="border-orange-500 text-orange-600">
        {daysLeft}{" 天后过期"}
      </Badge>
    );
  }

  return (
    <Badge variant="default" className="bg-green-600">
      {"有效"}
    </Badge>
  );
}

// Format expiry date
function formatExpiryDate(timestamp: number): string {
  if (!timestamp) return "-";
  const date = new Date(timestamp * 1000);
  return date.toLocaleDateString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  });
}

export default function AdminMembersPage() {
  const params = useParams();
  const userUUID = params.uuid as string;

  // State
  const [members, setMembers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Add member dialog state
  const [addDialogOpen, setAddDialogOpen] = useState(false);
  const [newMemberEmail, setNewMemberEmail] = useState("");
  const [adding, setAdding] = useState(false);

  // Remove member confirmation state
  const [removeDialogOpen, setRemoveDialogOpen] = useState(false);
  const [memberToRemove, setMemberToRemove] = useState<User | null>(null);
  const [removing, setRemoving] = useState(false);

  // Fetch members
  const fetchMembers = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const response = await api.getAdminMembers(userUUID);
      setMembers(response.items || []);
    } catch (err) {
      console.error("Failed to fetch members:", err);
      setError("获取成员列表失败");
      if (err instanceof ApiError && !err.isUnauthorized()) {
        toast.error("获取成员列表失败，请重试");
      }
    } finally {
      setLoading(false);
    }
  }, [userUUID]);

  // Handle add member
  const handleAddMember = async () => {
    const email = newMemberEmail.trim();

    if (!email) {
      toast.warning("请输入邮箱地址");
      return;
    }

    if (!isValidEmail(email)) {
      toast.warning("请输入有效的邮箱地址");
      return;
    }

    setAdding(true);
    try {
      const request: AddMemberRequest = { memberEmail: email };
      const newMember = await api.addAdminMember(userUUID, request);

      // Add to local state
      setMembers((prev) => [...prev, newMember]);

      // Close dialog and reset
      setAddDialogOpen(false);
      setNewMemberEmail("");

      toast.success("成员添加成功");
    } catch (err) {
      console.error("Failed to add member:", err);
      if (err instanceof ApiError) {
        if (err.code === ErrorCode.InvalidArgument) {
          toast.error("该邮箱已被使用");
        } else if (!err.isUnauthorized()) {
          toast.error(err.message || "添加成员失败");
        }
      }
    } finally {
      setAdding(false);
    }
  };

  // Handle remove member
  const handleRemoveMemberConfirm = async () => {
    if (!memberToRemove) return;

    setRemoving(true);
    try {
      await api.removeAdminMember(userUUID, memberToRemove.uuid);

      // Remove from local state
      setMembers((prev) => prev.filter((m) => m.uuid !== memberToRemove.uuid));

      // Close dialog
      setRemoveDialogOpen(false);
      setMemberToRemove(null);

      toast.success("成员已移除");
    } catch (err) {
      console.error("Failed to remove member:", err);
      if (err instanceof ApiError && !err.isUnauthorized()) {
        toast.error(err.message || "移除成员失败");
      }
    } finally {
      setRemoving(false);
    }
  };

  // Open remove dialog
  const handleRemoveMember = (member: User) => {
    setMemberToRemove(member);
    setRemoveDialogOpen(true);
  };

  // Initial fetch
  useEffect(() => {
    if (userUUID) {
      fetchMembers();
    }
  }, [userUUID, fetchMembers]);

  return (
    <div className="container mx-auto py-6 px-4">
      {/* Back navigation */}
      <div className="mb-6">
        <Link href="/manager/users">
          <Button variant="ghost" size="sm">
            <ArrowLeft className="h-4 w-4 mr-2" />
            {"返回"}
          </Button>
        </Link>
      </div>

      {/* Header */}
      <Card className="mb-6">
        <CardHeader>
          <div className="flex items-center justify-between">
            <div className="space-y-1">
              <CardTitle className="flex items-center gap-2">
                <UserIcon className="h-5 w-5" />
                {"成员管理"}
              </CardTitle>
              <CardDescription>
                {"管理用户的共享成员"} {"(UUID: "}
                {userUUID.slice(0, 8)}
                {"...)"}
              </CardDescription>
            </div>
            <div className="flex gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={fetchMembers}
                disabled={loading}
              >
                <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
              </Button>
              <Button
                size="sm"
                onClick={() => setAddDialogOpen(true)}
                disabled={loading}
              >
                <Plus className="h-4 w-4 mr-2" />
                {"添加成员"}
              </Button>
            </div>
          </div>
        </CardHeader>
      </Card>

      {/* Loading State */}
      {loading && (
        <Card>
          <CardContent className="flex items-center justify-center py-12">
            <div className="flex flex-col items-center gap-2">
              <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
              <p className="text-sm text-muted-foreground">
                {"加载中..."}
              </p>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Error State */}
      {!loading && error && (
        <Card>
          <CardContent className="py-6">
            <div className="flex items-start gap-4">
              <AlertCircle className="h-5 w-5 text-destructive mt-0.5" />
              <div className="flex-1">
                <p className="text-sm font-medium text-destructive">{error}</p>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={fetchMembers}
                  className="mt-4"
                >
                  {"重试"}
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Empty State */}
      {!loading && !error && members.length === 0 && (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-12">
            <Users className="h-12 w-12 text-muted-foreground mb-4" />
            <h3 className="text-lg font-semibold mb-2">
              {"暂无成员"}
            </h3>
            <p className="text-sm text-muted-foreground mb-4">
              {"该用户还没有添加任何共享成员"}
            </p>
            <Button onClick={() => setAddDialogOpen(true)}>
              <Plus className="h-4 w-4 mr-2" />
              {"添加第一个成员"}
            </Button>
          </CardContent>
        </Card>
      )}

      {/* Members List */}
      {!loading && !error && members.length > 0 && (
        <div className="grid gap-4">
          {members.map((member) => {
            const memberEmail =
              member.loginIdentifies?.find((li) => li.type === "email")?.value ||
              "无邮箱";

            return (
              <Card key={member.uuid}>
                <CardContent className="pt-6">
                  <div className="flex items-start justify-between">
                    <div className="flex-1 space-y-3">
                      {/* Email */}
                      <div className="flex items-center gap-2">
                        <Mail className="h-4 w-4 text-muted-foreground" />
                        <span className="font-medium">{memberEmail}</span>
                      </div>

                      {/* Status Badge */}
                      <div>{getMemberStatusBadge(member)}</div>

                      {/* Expiry Date & UUID */}
                      <div className="flex flex-col gap-1 text-sm text-muted-foreground">
                        <div className="flex items-center gap-2">
                          <Clock className="h-3 w-3" />
                          <span>
                            {"过期时间: "}
                            {formatExpiryDate(member.expiredAt)}
                          </span>
                        </div>
                        <div className="text-xs">
                          {"UUID: "}
                          {member.uuid.slice(0, 8)}
                          {"..."}
                        </div>
                      </div>
                    </div>

                    {/* Actions */}
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => handleRemoveMember(member)}
                      className="text-destructive hover:text-destructive"
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                </CardContent>
              </Card>
            );
          })}
        </div>
      )}

      {/* Add Member Dialog */}
      <Dialog open={addDialogOpen} onOpenChange={setAddDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{"添加成员"}</DialogTitle>
            <DialogDescription>
              {"输入新成员的邮箱地址，该成员将可以使用此账户的订阅权限"}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label htmlFor="email">{"邮箱地址"}</Label>
              <Input
                id="email"
                type="email"
                placeholder={"请输入邮箱地址"}
                value={newMemberEmail}
                onChange={(e) => setNewMemberEmail(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !adding) {
                    handleAddMember();
                  }
                }}
                disabled={adding}
                autoFocus
              />
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                setAddDialogOpen(false);
                setNewMemberEmail("");
              }}
              disabled={adding}
            >
              {"取消"}
            </Button>
            <Button
              onClick={handleAddMember}
              disabled={adding || !newMemberEmail.trim()}
            >
              {adding && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              {adding ? "添加中..." : "添加"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Remove Member Confirmation Dialog */}
      <Dialog open={removeDialogOpen} onOpenChange={setRemoveDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {"移除成员"}
            </DialogTitle>
            <DialogDescription>
              {"确定要移除该成员吗？移除后该成员将无法使用此账户的订阅权限。"}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setRemoveDialogOpen(false)}
              disabled={removing}
            >
              {"取消"}
            </Button>
            <Button
              variant="destructive"
              onClick={handleRemoveMemberConfirm}
              disabled={removing}
            >
              {removing && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              {removing ? "移除中..." : "移除"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

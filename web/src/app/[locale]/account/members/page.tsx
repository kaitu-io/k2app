"use client";

export const dynamic = "force-dynamic";

import { useState, useEffect, useCallback } from "react";
import { useTranslations } from "next-intl";
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Users,
  Plus,
  Trash2,
  RefreshCw,
  Mail,
  Clock,
  AlertCircle,
  Loader2,
} from "lucide-react";

// Email validation helper
function isValidEmail(email: string): boolean {
  const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
  return emailRegex.test(email.trim());
}

// Get member status badge
function getMemberStatusBadge(member: User, t: (key: string) => string) {
  if (!member.expiredAt) {
    return (
      <Badge variant="secondary">
        {t("admin.account.members.status.notActivated")}
      </Badge>
    );
  }

  const expiredAt = new Date(member.expiredAt * 1000);
  const now = new Date();

  if (expiredAt <= now) {
    return (
      <Badge variant="destructive">
        {t("admin.account.members.status.expired")}
      </Badge>
    );
  }

  const daysLeft = Math.ceil((expiredAt.getTime() - now.getTime()) / (1000 * 60 * 60 * 24));
  if (daysLeft <= 7) {
    return (
      <Badge variant="outline" className="border-orange-500 text-orange-600">
        {daysLeft} {t("purchase.purchase.dash")} {t("admin.account.members.status.expiringSoon")}
      </Badge>
    );
  }

  return (
    <Badge variant="default" className="bg-green-600">
      {t("admin.account.members.status.valid")}
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

export default function MembersPage() {
  const t = useTranslations();

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
      const response = await api.getMembers();
      setMembers(response.items || []);
    } catch (err) {
      console.error("Failed to fetch members:", err);
      setError(t("admin.account.members.getMembersFailed"));
      if (err instanceof ApiError && !err.isUnauthorized()) {
        toast.error(t("admin.account.members.getMembersFailedRetry"));
      }
    } finally {
      setLoading(false);
    }
  }, [t]);

  // Handle add member
  const handleAddMember = async () => {
    const email = newMemberEmail.trim();

    if (!email) {
      toast.warning(t("admin.account.members.emailRequired"));
      return;
    }

    if (!isValidEmail(email)) {
      toast.warning(t("admin.account.members.invalidEmail"));
      return;
    }

    setAdding(true);
    try {
      const request: AddMemberRequest = { memberEmail: email };
      const newMember = await api.addMember(request);

      // Add to local state
      setMembers((prev) => [...prev, newMember]);

      // Close dialog and reset
      setAddDialogOpen(false);
      setNewMemberEmail("");

      toast.success(t("admin.account.members.addMemberSuccess"));
    } catch (err) {
      console.error("Failed to add member:", err);
      if (err instanceof ApiError) {
        if (err.code === ErrorCode.InvalidArgument) {
          toast.error(t("admin.account.members.emailAlreadyInUse"));
        } else if (!err.isUnauthorized()) {
          toast.error(err.message || t("admin.account.members.addMemberFailed"));
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
      await api.removeMember(memberToRemove.uuid);

      // Remove from local state
      setMembers((prev) => prev.filter((m) => m.uuid !== memberToRemove.uuid));

      // Close dialog
      setRemoveDialogOpen(false);
      setMemberToRemove(null);

      toast.success(t("admin.account.members.removeMemberSuccess"));
    } catch (err) {
      console.error("Failed to remove member:", err);
      if (err instanceof ApiError && !err.isUnauthorized()) {
        toast.error(err.message || t("admin.account.members.removeMemberFailed"));
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
    fetchMembers();
  }, [fetchMembers]);

  return (
    <div className="space-y-6">
      {/* Header */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div className="space-y-1">
              <CardTitle className="flex items-center gap-2">
                <Users className="h-5 w-5" />
                {t("admin.account.members.title")}
              </CardTitle>
              <CardDescription>
                {t("admin.account.members.description")}
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
                {t("admin.account.members.addMember")}
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
                {t("admin.account.members.loading")}
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
                  {t("common.common.retry")}
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
              {t("admin.account.members.noMembers")}
            </h3>
            <p className="text-sm text-muted-foreground mb-4">
              {t("admin.account.members.noMembersDesc")}
            </p>
            <Button onClick={() => setAddDialogOpen(true)}>
              <Plus className="h-4 w-4 mr-2" />
              {t("admin.account.members.addFirstMember")}
            </Button>
          </CardContent>
        </Card>
      )}

      {/* Members List - Desktop Table View */}
      {!loading && !error && members.length > 0 && (
        <>
          {/* Desktop View */}
          <Card className="hidden md:block">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("admin.account.members.memberEmail")}</TableHead>
                  <TableHead>{t("admin.account.members.userId")}</TableHead>
                  <TableHead>{t("admin.account.members.memberStatus")}</TableHead>
                  <TableHead>{t("admin.account.members.expiryDate")}</TableHead>
                  <TableHead className="text-right">{t("common.common.actions")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {members.map((member) => {
                  const memberEmail =
                    member.loginIdentifies?.find((li) => li.type === "email")?.value ||
                    t("admin.account.members.noEmail");

                  return (
                    <TableRow key={member.uuid}>
                      <TableCell className="font-medium">
                        <div className="flex items-center gap-2">
                          <Mail className="h-4 w-4 text-muted-foreground" />
                          {memberEmail}
                        </div>
                      </TableCell>
                      <TableCell>
                        <span className="font-mono text-xs text-muted-foreground">{member.uuid}</span>
                      </TableCell>
                      <TableCell>{getMemberStatusBadge(member, t)}</TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2 text-sm text-muted-foreground">
                          <Clock className="h-3 w-3" />
                          {formatExpiryDate(member.expiredAt)}
                        </div>
                      </TableCell>
                      <TableCell className="text-right">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleRemoveMember(member)}
                          className="text-destructive hover:text-destructive"
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </Card>

          {/* Mobile View - Card List */}
          <div className="md:hidden grid gap-4">
            {members.map((member) => {
              const memberEmail =
                member.loginIdentifies?.find((li) => li.type === "email")?.value ||
                t("admin.account.members.noEmail");

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
                        <div>{getMemberStatusBadge(member, t)}</div>

                        {/* Expiry Date & User ID */}
                        <div className="flex flex-col gap-2 text-sm text-muted-foreground">
                          <div className="flex items-center gap-2">
                            <Clock className="h-3 w-3" />
                            <span>
                              {t("admin.account.members.expiryDate")}
                              {": "}
                              {formatExpiryDate(member.expiredAt)}
                            </span>
                          </div>
                          <div className="text-xs font-mono text-muted-foreground">
                            {t("admin.account.members.userId")}{": "}
                            {member.uuid}
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
        </>
      )}

      {/* Add Member Dialog */}
      <Dialog open={addDialogOpen} onOpenChange={setAddDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("admin.account.members.addMemberDialog")}</DialogTitle>
            <DialogDescription>
              {t("admin.account.members.emailHelp")}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label htmlFor="email">{t("admin.account.members.emailLabel")}</Label>
              <Input
                id="email"
                type="email"
                placeholder={t("admin.account.members.emailPlaceholder")}
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
              {t("common.common.cancel")}
            </Button>
            <Button
              onClick={handleAddMember}
              disabled={adding || !newMemberEmail.trim()}
            >
              {adding && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              {adding ? t("common.common.adding") : t("common.common.add")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Remove Member Confirmation Dialog */}
      <Dialog open={removeDialogOpen} onOpenChange={setRemoveDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t("admin.account.members.removeMember")}
            </DialogTitle>
            <DialogDescription>
              {t("admin.account.members.removeMemberConfirm")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setRemoveDialogOpen(false)}
              disabled={removing}
            >
              {t("common.common.cancel")}
            </Button>
            <Button
              variant="destructive"
              onClick={handleRemoveMemberConfirm}
              disabled={removing}
            >
              {removing && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              {removing ? t("common.common.removing") : t("common.common.remove")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

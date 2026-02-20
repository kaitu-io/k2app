"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState, use } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { api, AdminRetailerDetailData, RetailerNote, getContactTypeName, getContactUrl, AdminUserSimple } from "@/lib/api";
import { format, addDays, addWeeks, setHours, setMinutes, startOfDay } from "date-fns";
import { toast } from "sonner";
import {
  ExternalLink,
  Check,
  Trash2,
  Edit2,
  Clock,
  MessageSquare,
  Wallet,
  Phone,
  AlertCircle,
  ChevronDown,
  Send,
  User,
  X,
} from "lucide-react";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

// Level color mapping
const levelColors: Record<number, string> = {
  1: '#9E9E9E',
  2: '#2196F3',
  3: '#9C27B0',
  4: '#FF9800',
};

// Level names
const levelNames: Record<number, string> = {
  1: 'Bronze',
  2: 'Silver',
  3: 'Gold',
  4: 'Platinum',
};

// Format amount
function formatAmount(cents: number): string {
  return `$${(cents / 100).toFixed(2)}`;
}

// Get tomorrow at 9am
function getTomorrow9am(): Date {
  const tomorrow = addDays(startOfDay(new Date()), 1);
  return setMinutes(setHours(tomorrow, 9), 0);
}

// Quick time options
function getQuickTimeOptions(): { label: string; value: Date }[] {
  const tomorrow9am = getTomorrow9am();
  return [
    { label: "1天后", value: tomorrow9am },
    { label: "2天后", value: addDays(tomorrow9am, 1) },
    { label: "1周后", value: addWeeks(tomorrow9am, 0) },
  ];
}

interface PageProps {
  params: Promise<{ uuid: string }>;
}

export default function RetailerDetailPage({ params }: PageProps) {
  const { uuid } = use(params);

  const [detail, setDetail] = useState<AdminRetailerDetailData | null>(null);
  const [notes, setNotes] = useState<RetailerNote[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [notesPage, setNotesPage] = useState(0);
  const [notesTotal, setNotesTotal] = useState(0);

  // Admin users for assignee dropdown
  const [adminUsers, setAdminUsers] = useState<AdminUserSimple[]>([]);

  // Level editing state
  const [isLevelDropdownOpen, setIsLevelDropdownOpen] = useState(false);
  const [isUpdatingLevel, setIsUpdatingLevel] = useState(false);

  // Inline add note form state
  const [newNoteContent, setNewNoteContent] = useState("");
  const [newNoteCommunicatedAt, setNewNoteCommunicatedAt] = useState(
    format(new Date(), "yyyy-MM-dd'T'HH:mm")
  );
  const [newNoteFollowUpAt, setNewNoteFollowUpAt] = useState("");
  const [newNoteAssigneeId, setNewNoteAssigneeId] = useState<number | undefined>(undefined);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [showFollowUpPicker, setShowFollowUpPicker] = useState(false);

  // Inline edit note state
  const [editingNote, setEditingNote] = useState<RetailerNote | null>(null);
  const [editContent, setEditContent] = useState("");
  const [editFollowUpAt, setEditFollowUpAt] = useState("");
  const [editAssigneeId, setEditAssigneeId] = useState<number | undefined>(undefined);

  // Delete confirmation dialog
  const [deletingNoteId, setDeletingNoteId] = useState<number | null>(null);

  // Load admin users
  useEffect(() => {
    const fetchAdminUsers = async () => {
      try {
        const users = await api.getAdminUsers();
        setAdminUsers(users);
      } catch (error) {
        console.error("Failed to fetch admin users:", error);
      }
    };
    fetchAdminUsers();
  }, []);

  // Load detail
  useEffect(() => {
    const fetchDetail = async () => {
      setIsLoading(true);
      try {
        const data = await api.getRetailerDetail(uuid);
        setDetail(data);
      } catch (error) {
        console.error("Failed to fetch retailer detail:", error);
        toast.error("加载分销商详情失败");
      } finally {
        setIsLoading(false);
      }
    };

    fetchDetail();
  }, [uuid]);

  // Load notes
  useEffect(() => {
    const fetchNotes = async () => {
      try {
        const result = await api.getRetailerNotes(uuid, {
          page: notesPage,
          pageSize: 20,
        });
        setNotes(result.items || []);
        if (result.pagination) {
          setNotesTotal(result.pagination.total);
        }
      } catch (error) {
        console.error("Failed to fetch notes:", error);
      }
    };

    fetchNotes();
  }, [uuid, notesPage]);

  // Handle level change
  const handleLevelChange = async (newLevel: number) => {
    if (!detail?.retailerConfig) return;
    if (newLevel === detail.retailerConfig.level) {
      setIsLevelDropdownOpen(false);
      return;
    }

    setIsUpdatingLevel(true);
    try {
      await api.updateRetailerLevel(uuid, { level: newLevel });
      // Update local state
      setDetail({
        ...detail,
        retailerConfig: {
          ...detail.retailerConfig,
          level: newLevel,
          levelName: levelNames[newLevel] || `L${newLevel}`,
        },
      });
      toast.success("分销商等级已更新");
      setIsLevelDropdownOpen(false);
    } catch (error) {
      console.error("Failed to update level:", error);
      toast.error("更新等级失败");
    } finally {
      setIsUpdatingLevel(false);
    }
  };

  // Add note (inline)
  const handleAddNote = async () => {
    if (!newNoteContent.trim()) {
      toast.error("请输入沟通内容");
      return;
    }

    setIsSubmitting(true);
    try {
      const note = await api.createRetailerNote(uuid, {
        content: newNoteContent,
        communicatedAt: Math.floor(new Date(newNoteCommunicatedAt).getTime() / 1000),
        followUpAt: newNoteFollowUpAt
          ? Math.floor(new Date(newNoteFollowUpAt).getTime() / 1000)
          : undefined,
        assigneeId: newNoteAssigneeId,
      });

      setNotes([note, ...notes]);
      setNotesTotal(notesTotal + 1);
      // Reset form
      setNewNoteContent("");
      setNewNoteFollowUpAt("");
      setNewNoteAssigneeId(undefined);
      setShowFollowUpPicker(false);
      toast.success("沟通记录已添加");
    } catch (error) {
      console.error("Failed to add note:", error);
      toast.error("添加沟通记录失败");
    } finally {
      setIsSubmitting(false);
    }
  };

  // Update note (inline)
  const handleUpdateNote = async () => {
    if (!editingNote) return;

    setIsSubmitting(true);
    try {
      const updated = await api.updateRetailerNote(uuid, editingNote.id, {
        content: editContent || undefined,
        followUpAt: editFollowUpAt
          ? Math.floor(new Date(editFollowUpAt).getTime() / 1000)
          : 0, // 0 means clear follow-up time
        assigneeId: editAssigneeId,
      });

      setNotes(notes.map((n) => (n.id === updated.id ? updated : n)));
      setEditingNote(null);
      toast.success("沟通记录已更新");
    } catch (error) {
      console.error("Failed to update note:", error);
      toast.error("更新沟通记录失败");
    } finally {
      setIsSubmitting(false);
    }
  };

  // Mark complete
  const handleMarkComplete = async (noteId: number) => {
    try {
      const updated = await api.updateRetailerNote(uuid, noteId, {
        isCompleted: true,
      });
      setNotes(notes.map((n) => (n.id === updated.id ? updated : n)));
      toast.success("已标记为完成");
    } catch (error) {
      console.error("Failed to mark complete:", error);
      toast.error("操作失败");
    }
  };

  // Delete note
  const handleDeleteNote = async () => {
    if (!deletingNoteId) return;

    try {
      await api.deleteRetailerNote(uuid, deletingNoteId);
      setNotes(notes.filter((n) => n.id !== deletingNoteId));
      setNotesTotal(notesTotal - 1);
      setDeletingNoteId(null);
      toast.success("沟通记录已删除");
    } catch (error) {
      console.error("Failed to delete note:", error);
      toast.error("删除沟通记录失败");
    }
  };

  // Start editing a note
  const startEditNote = (note: RetailerNote) => {
    setEditingNote(note);
    setEditContent(note.content);
    setEditFollowUpAt(
      note.followUpAt
        ? format(new Date(note.followUpAt * 1000), "yyyy-MM-dd'T'HH:mm")
        : ""
    );
    setEditAssigneeId(note.assigneeId);
  };

  // Cancel editing
  const cancelEdit = () => {
    setEditingNote(null);
    setEditContent("");
    setEditFollowUpAt("");
    setEditAssigneeId(undefined);
  };

  // Apply quick time selection
  const applyQuickTime = (date: Date) => {
    setNewNoteFollowUpAt(format(date, "yyyy-MM-dd'T'HH:mm"));
    setShowFollowUpPicker(false);
  };

  if (isLoading) {
    return (
      <div className="container mx-auto py-10">
        <div className="text-center">加载中...</div>
      </div>
    );
  }

  if (!detail) {
    return (
      <div className="container mx-auto py-10">
        <div className="text-center">分销商不存在</div>
      </div>
    );
  }

  const config = detail.retailerConfig;
  const wallet = detail.wallet;

  return (
    <div className="container mx-auto py-10 space-y-6">
      {/* Header with level editing */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">{detail.email}</h1>
          <div className="flex items-center gap-2 mt-2">
            {config && (
              <DropdownMenu open={isLevelDropdownOpen} onOpenChange={setIsLevelDropdownOpen}>
                <DropdownMenuTrigger asChild>
                  <button
                    className="inline-flex items-center gap-1 px-2 py-1 rounded-full text-white text-sm font-medium cursor-pointer hover:opacity-80 transition-opacity"
                    style={{ backgroundColor: levelColors[config.level] || '#9E9E9E' }}
                    disabled={isUpdatingLevel}
                  >
                    L{config.level} {config.levelName}
                    <ChevronDown className="h-3 w-3" />
                  </button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start">
                  {[1, 2, 3, 4].map((level) => (
                    <DropdownMenuItem
                      key={level}
                      onClick={() => handleLevelChange(level)}
                      className="flex items-center gap-2"
                    >
                      <Badge
                        style={{ backgroundColor: levelColors[level] }}
                        className="text-white"
                      >
                        L{level}
                      </Badge>
                      {levelNames[level]}
                      {config.level === level && <Check className="h-4 w-4 ml-auto" />}
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
            )}
            {detail.pendingFollowUps > 0 && (
              <Badge variant="destructive">
                <AlertCircle className="h-3 w-3 mr-1" />
                {detail.pendingFollowUps} 个待跟进
              </Badge>
            )}
          </div>
        </div>
        <Button
          variant="outline"
          onClick={() => window.open(`/manager/users/detail?uuid=${uuid}`, '_blank')}
        >
          <ExternalLink className="h-4 w-4 mr-2" />
          查看用户详情
        </Button>
      </div>

      {/* Card row */}
      <div className="grid gap-6 md:grid-cols-3">
        {/* Retailer config card */}
        {config && (
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Badge
                  style={{ backgroundColor: levelColors[config.level] }}
                  className="text-white"
                >
                  L{config.level}
                </Badge>
                {config.levelName}
              </CardTitle>
              <CardDescription>分销商配置</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <div className="text-sm text-muted-foreground">首单分成</div>
                  <div className="text-xl font-bold">{config.firstOrderPercent}%</div>
                </div>
                <div>
                  <div className="text-sm text-muted-foreground">续费分成</div>
                  <div className="text-xl font-bold">{config.renewalPercent}%</div>
                </div>
              </div>
              <div>
                <div className="text-sm text-muted-foreground mb-1">付费用户数</div>
                <div className="text-lg font-medium">{config.paidUserCount}</div>
              </div>
              {config.nextLevel && (
                <div>
                  <div className="flex justify-between text-sm mb-1">
                    <span className="text-muted-foreground">升级到 {config.nextLevelName}</span>
                    <span>{config.progressPercent}%</span>
                  </div>
                  <Progress value={config.progressPercent} className="h-2" />
                  <div className="text-xs text-muted-foreground mt-1">
                    还需 {(config.nextLevelRequirement || 0) - config.paidUserCount} 个付费用户
                  </div>
                </div>
              )}
            </CardContent>
          </Card>
        )}

        {/* Contact card */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Phone className="h-5 w-5" />
              联系方式
            </CardTitle>
          </CardHeader>
          <CardContent>
            {config?.contacts && config.contacts.length > 0 ? (
              <div className="space-y-3">
                {config.contacts.map((contact, idx) => {
                  const url = getContactUrl(contact);
                  return (
                    <div key={idx} className="flex items-center justify-between p-3 bg-muted rounded-lg">
                      <div>
                        <div className="font-medium">{getContactTypeName(contact.type)}</div>
                        <div className="text-sm text-muted-foreground">{contact.value}</div>
                      </div>
                      {url && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => window.open(url, '_blank')}
                        >
                          <ExternalLink className="h-4 w-4" />
                        </Button>
                      )}
                    </div>
                  );
                })}
              </div>
            ) : (
              <div className="text-muted-foreground text-center py-4">
                暂无联系方式
              </div>
            )}
          </CardContent>
        </Card>

        {/* Wallet info card */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Wallet className="h-5 w-5" />
              钱包信息
            </CardTitle>
          </CardHeader>
          <CardContent>
            {wallet ? (
              <div className="space-y-4">
                <div>
                  <div className="text-sm text-muted-foreground">可用余额</div>
                  <div className="text-2xl font-bold text-green-600">
                    {formatAmount(wallet.availableBalance)}
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-4 text-sm">
                  <div>
                    <div className="text-muted-foreground">冻结余额</div>
                    <div className="font-medium">{formatAmount(wallet.frozenBalance)}</div>
                  </div>
                  <div>
                    <div className="text-muted-foreground">总余额</div>
                    <div className="font-medium">{formatAmount(wallet.balance)}</div>
                  </div>
                  <div>
                    <div className="text-muted-foreground">累计收入</div>
                    <div className="font-medium">{formatAmount(wallet.totalIncome)}</div>
                  </div>
                  <div>
                    <div className="text-muted-foreground">累计提现</div>
                    <div className="font-medium">{formatAmount(wallet.totalWithdrawn)}</div>
                  </div>
                </div>
              </div>
            ) : (
              <div className="text-muted-foreground text-center py-4">
                暂无钱包信息
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Communication records */}
      <Card>
        <CardHeader>
          <div>
            <CardTitle className="flex items-center gap-2">
              <MessageSquare className="h-5 w-5" />
              沟通记录
            </CardTitle>
            <CardDescription>共 {notesTotal} 条记录</CardDescription>
          </div>
        </CardHeader>
        <CardContent className="space-y-6">
          {/* Inline add note form */}
          <div className="p-4 rounded-lg border bg-muted/30 space-y-4">
            <div className="space-y-2">
              <Textarea
                placeholder="输入沟通内容..."
                value={newNoteContent}
                onChange={(e) => setNewNoteContent(e.target.value)}
                rows={3}
                className="resize-none"
              />
            </div>
            <div className="flex flex-wrap items-center gap-3">
              {/* Communication time */}
              <div className="flex items-center gap-2">
                <Label className="text-sm text-muted-foreground whitespace-nowrap">沟通时间</Label>
                <Input
                  type="datetime-local"
                  value={newNoteCommunicatedAt}
                  onChange={(e) => setNewNoteCommunicatedAt(e.target.value)}
                  className="w-auto"
                />
              </div>

              {/* Follow-up time with quick options */}
              <div className="flex items-center gap-2">
                <Label className="text-sm text-muted-foreground whitespace-nowrap">跟进</Label>
                {!showFollowUpPicker && !newNoteFollowUpAt ? (
                  <div className="flex gap-1">
                    {getQuickTimeOptions().map((opt) => (
                      <Button
                        key={opt.label}
                        variant="outline"
                        size="sm"
                        onClick={() => applyQuickTime(opt.value)}
                      >
                        {opt.label}
                      </Button>
                    ))}
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setShowFollowUpPicker(true)}
                    >
                      自定义
                    </Button>
                  </div>
                ) : (
                  <div className="flex items-center gap-1">
                    <Input
                      type="datetime-local"
                      value={newNoteFollowUpAt}
                      onChange={(e) => setNewNoteFollowUpAt(e.target.value)}
                      className="w-auto"
                    />
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        setNewNoteFollowUpAt("");
                        setShowFollowUpPicker(false);
                      }}
                    >
                      <X className="h-4 w-4" />
                    </Button>
                  </div>
                )}
              </div>

              {/* Assignee selector */}
              {newNoteFollowUpAt && (
                <div className="flex items-center gap-2">
                  <Label className="text-sm text-muted-foreground whitespace-nowrap">
                    <User className="h-4 w-4 inline mr-1" />
                    跟进人
                  </Label>
                  <Select
                    value={newNoteAssigneeId?.toString() || ""}
                    onValueChange={(v) => setNewNoteAssigneeId(v ? parseInt(v) : undefined)}
                  >
                    <SelectTrigger className="w-[140px]">
                      <SelectValue placeholder="选择跟进人" />
                    </SelectTrigger>
                    <SelectContent>
                      {adminUsers.map((user) => (
                        <SelectItem key={user.id} value={user.id.toString()}>
                          {user.email.split('@')[0]}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              )}

              {/* Submit button */}
              <Button
                onClick={handleAddNote}
                disabled={isSubmitting || !newNoteContent.trim()}
                className="ml-auto"
              >
                <Send className="h-4 w-4 mr-2" />
                {isSubmitting ? "提交中..." : "添加记录"}
              </Button>
            </div>
          </div>

          {/* Notes list */}
          {notes.length > 0 ? (
            <div className="space-y-4">
              {notes.map((note) => (
                <div
                  key={note.id}
                  className={`p-4 rounded-lg border ${
                    note.isOverdue ? 'border-orange-300 bg-orange-50' : 'bg-muted/50'
                  }`}
                >
                  {editingNote?.id === note.id ? (
                    // Inline edit form
                    <div className="space-y-4">
                      <Textarea
                        value={editContent}
                        onChange={(e) => setEditContent(e.target.value)}
                        rows={3}
                        className="resize-none"
                      />
                      <div className="flex flex-wrap items-center gap-3">
                        <div className="flex items-center gap-2">
                          <Label className="text-sm text-muted-foreground whitespace-nowrap">跟进时间</Label>
                          <Input
                            type="datetime-local"
                            value={editFollowUpAt}
                            onChange={(e) => setEditFollowUpAt(e.target.value)}
                            className="w-auto"
                          />
                        </div>
                        {editFollowUpAt && (
                          <div className="flex items-center gap-2">
                            <Label className="text-sm text-muted-foreground whitespace-nowrap">
                              <User className="h-4 w-4 inline mr-1" />
                              跟进人
                            </Label>
                            <Select
                              value={editAssigneeId?.toString() || ""}
                              onValueChange={(v) => setEditAssigneeId(v ? parseInt(v) : undefined)}
                            >
                              <SelectTrigger className="w-[140px]">
                                <SelectValue placeholder="选择跟进人" />
                              </SelectTrigger>
                              <SelectContent>
                                {adminUsers.map((user) => (
                                  <SelectItem key={user.id} value={user.id.toString()}>
                                    {user.email.split('@')[0]}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                          </div>
                        )}
                        <div className="flex gap-2 ml-auto">
                          <Button variant="outline" size="sm" onClick={cancelEdit}>
                            取消
                          </Button>
                          <Button size="sm" onClick={handleUpdateNote} disabled={isSubmitting}>
                            {isSubmitting ? "保存中..." : "保存"}
                          </Button>
                        </div>
                      </div>
                    </div>
                  ) : (
                    // Display mode
                    <div className="flex items-start justify-between">
                      <div className="flex-1">
                        <div className="flex items-center gap-2 mb-2 flex-wrap">
                          <span className="text-sm text-muted-foreground">
                            {format(new Date(note.communicatedAt * 1000), "yyyy-MM-dd HH:mm")}
                          </span>
                          {note.operatorName && (
                            <Badge variant="outline">{note.operatorName}</Badge>
                          )}
                          {note.followUpAt && !note.isCompleted && (
                            <Badge variant={note.isOverdue ? "destructive" : "secondary"}>
                              <Clock className="h-3 w-3 mr-1" />
                              跟进: {format(new Date(note.followUpAt * 1000), "MM-dd HH:mm")}
                              {note.isOverdue && ` (逾期${note.daysOverdue}天)`}
                            </Badge>
                          )}
                          {note.assigneeName && (
                            <Badge variant="outline" className="text-blue-600">
                              <User className="h-3 w-3 mr-1" />
                              {note.assigneeName}
                            </Badge>
                          )}
                          {note.isCompleted && (
                            <Badge variant="outline" className="text-green-600">
                              <Check className="h-3 w-3 mr-1" />
                              已完成
                            </Badge>
                          )}
                        </div>
                        <p className="whitespace-pre-wrap">{note.content}</p>
                      </div>
                      <div className="flex items-center gap-1 ml-4">
                        {note.followUpAt && !note.isCompleted && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleMarkComplete(note.id)}
                            title="标记完成"
                          >
                            <Check className="h-4 w-4" />
                          </Button>
                        )}
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => startEditNote(note)}
                          title="编辑"
                        >
                          <Edit2 className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setDeletingNoteId(note.id)}
                          title="删除"
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </div>
                    </div>
                  )}
                </div>
              ))}

              {/* Pagination */}
              {notesTotal > 20 && (
                <div className="flex justify-center gap-2 pt-4">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setNotesPage(notesPage - 1)}
                    disabled={notesPage === 0}
                  >
                    上一页
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setNotesPage(notesPage + 1)}
                    disabled={(notesPage + 1) * 20 >= notesTotal}
                  >
                    下一页
                  </Button>
                </div>
              )}
            </div>
          ) : (
            <div className="text-muted-foreground text-center py-8">
              暂无沟通记录
            </div>
          )}
        </CardContent>
      </Card>

      {/* Delete confirmation dialog */}
      <AlertDialog open={!!deletingNoteId} onOpenChange={(open) => !open && setDeletingNoteId(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除</AlertDialogTitle>
            <AlertDialogDescription>
              确定要删除这条沟通记录吗？此操作不可撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleDeleteNote}>删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

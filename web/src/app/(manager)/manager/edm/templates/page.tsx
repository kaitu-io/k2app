"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState, useCallback } from "react";
import {
  ColumnDef,
  flexRender,
  getCoreRowModel,
  useReactTable,
  getPaginationRowModel,
} from "@tanstack/react-table";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/lib/api";
import { toast } from "sonner";
import { Plus, FileText, Edit, Trash2, Send } from "lucide-react";
import type { EmailTemplateResponse } from "@/lib/api";
import Link from "next/link";

// Simple Chinese translation helper for admin pages (no i18n dependency)
const t = (key: string) => {
  const map: Record<string, string> = {
    "campaigns.edm.templates.title": "邮件模板",
    "campaigns.edm.templates.subtitle": "管理邮件模板",
    "campaigns.edm.templates.name": "名称",
    "campaigns.edm.templates.language": "语言",
    "campaigns.edm.templates.isActive": "状态",
    "campaigns.edm.templates.active": "启用",
    "campaigns.edm.templates.inactive": "停用",
    "campaigns.edm.templates.createdAt": "创建时间",
    "campaigns.edm.templates.actions": "操作",
    "campaigns.edm.templates.createTask": "创建任务",
    "campaigns.edm.templates.edit": "编辑",
    "campaigns.edm.templates.delete": "删除",
    "campaigns.edm.templates.deleteSuccess": "删除成功",
    "campaigns.edm.templates.deleteFailed": "删除失败",
    "campaigns.edm.templates.fetchFailed": "获取模板列表失败",
    "campaigns.edm.templates.loading": "加载中...",
    "campaigns.edm.templates.templateList": "模板列表",
    "campaigns.edm.templates.createTemplate": "创建模板",
    "campaigns.edm.templates.noTemplates": "暂无模板",
    "campaigns.edm.templates.previousPage": "上一页",
    "campaigns.edm.templates.nextPage": "下一页",
    "campaigns.edm.templates.languages.zh-CN": "简体中文",
    "campaigns.edm.templates.languages.zh-TW": "繁体中文",
    "campaigns.edm.templates.languages.zh-HK": "香港繁体",
    "campaigns.edm.templates.languages.en-US": "美式英语",
    "campaigns.edm.templates.languages.en-GB": "英式英语",
    "campaigns.edm.templates.languages.en-AU": "澳洲英语",
    "campaigns.edm.templates.languages.ja": "日语",
  };
  return map[key] || key.split('.').pop() || key;
};

export default function EmailTemplatesPage() {
  const locale = "zh-CN";
  const [templates, setTemplates] = useState<EmailTemplateResponse[]>([]);
  const [loading, setLoading] = useState(true);


  const languageOptions = [
    { value: "zh-CN", label: t("campaigns.edm.templates.languages.zh-CN") },
    { value: "zh-TW", label: t("campaigns.edm.templates.languages.zh-TW") },
    { value: "zh-HK", label: t("campaigns.edm.templates.languages.zh-HK") },
    { value: "en-US", label: t("campaigns.edm.templates.languages.en-US") },
    { value: "en-GB", label: t("campaigns.edm.templates.languages.en-GB") },
    { value: "en-AU", label: t("campaigns.edm.templates.languages.en-AU") },
    { value: "ja", label: t("campaigns.edm.templates.languages.ja") },
  ];

  const formatDate = (timestamp: number) => {
    if (!timestamp) return '-';
    return new Date(timestamp * 1000).toLocaleDateString(locale);
  };

  const columns: ColumnDef<EmailTemplateResponse>[] = [
    {
      accessorKey: "name",
      header: t("campaigns.edm.templates.name"),
      cell: ({ row }) => (
        <div>
          <div className="font-medium">{row.getValue("name")}</div>
          <div className="text-sm text-muted-foreground">
            {row.original.subject}
          </div>
        </div>
      ),
    },
    {
      accessorKey: "language",
      header: t("campaigns.edm.templates.language"),
      cell: ({ row }) => {
        const language = row.getValue("language") as string;
        const langOption = languageOptions.find(l => l.value === language);
        return <Badge variant="outline">{langOption?.label || language}</Badge>;
      },
    },
    {
      accessorKey: "isActive",
      header: t("campaigns.edm.templates.isActive"),
      cell: ({ row }) => (
        <Badge variant={row.getValue("isActive") ? "default" : "secondary"}>
          {row.getValue("isActive") ? t("campaigns.edm.templates.active") : t("campaigns.edm.templates.inactive")}
        </Badge>
      ),
    },
    {
      accessorKey: "createdAt",
      header: t("campaigns.edm.templates.createdAt"),
      cell: ({ row }) => {
        return formatDate(row.getValue("createdAt"));
      },
    },
    {
      id: "actions",
      header: t("campaigns.edm.templates.actions"),
      cell: ({ row }) => (
        <div className="flex space-x-2">
          <Button
            variant="default"
            size="sm"
            asChild
            title={t("campaigns.edm.templates.createTask")}
          >
            <Link href={`/manager/edm/tasks/create?templateId=${row.original.id}`}>
              <Send className="h-4 w-4" />
            </Link>
          </Button>
          <Button
            variant="outline"
            size="sm"
            asChild
            title={t("campaigns.edm.templates.edit")}
          >
            <Link href={`/manager/edm/templates/editor?id=${row.original.id}`}>
              <Edit className="h-4 w-4" />
            </Link>
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => handleDelete(row.original)}
            title={t("campaigns.edm.templates.delete")}
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      ),
    },
  ];

  const table = useReactTable({
    data: templates,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    initialState: {
      pagination: {
        pageSize: 10,
      },
    },
  });

  const fetchTemplates = useCallback(async () => {
    try {
      setLoading(true);
      const data = await api.getEmailTemplates();
      setTemplates(data.items);
    } catch (error) {
      toast.error(t("campaigns.edm.templates.fetchFailed"));
      console.error("Error fetching templates:", error);
    } finally {
      setLoading(false);
    }
  }, []);


  const handleDelete = async (template: EmailTemplateResponse) => {
    if (!confirm(`确定删除模板 "${template.name}" 吗？`)) {
      return;
    }

    try {
      await api.deleteEmailTemplate(template.id);

      toast.success(t("campaigns.edm.templates.deleteSuccess"));
      fetchTemplates();
    } catch (error) {
      toast.error(t("campaigns.edm.templates.deleteFailed"));
      console.error("Error deleting template:", error);
    }
  };


  useEffect(() => {
    fetchTemplates();
  }, [fetchTemplates]);

  if (loading) {
    return <div>{t("campaigns.edm.templates.loading")}</div>;
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <Link href="/manager/edm" className="text-3xl font-bold tracking-tight hover:underline">
            {t("campaigns.edm.templates.title")}
          </Link>
          <p className="text-muted-foreground">
            {t("campaigns.edm.templates.subtitle")}
          </p>
        </div>
        <Button asChild>
          <Link href="/manager/edm/templates/editor">
            <Plus className="mr-2 h-4 w-4" />
            {t("campaigns.edm.templates.createTemplate")}
          </Link>
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center">
            <FileText className="mr-2 h-5 w-5" />
            {t("campaigns.edm.templates.templateList")}
          </CardTitle>
          <CardDescription>
            {`共 ${templates.length} 个模板`}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              {table.getHeaderGroups().map((headerGroup) => (
                <TableRow key={headerGroup.id}>
                  {headerGroup.headers.map((header) => (
                    <TableHead key={header.id}>
                      {header.isPlaceholder
                        ? null
                        : flexRender(
                            header.column.columnDef.header,
                            header.getContext()
                          )}
                    </TableHead>
                  ))}
                </TableRow>
              ))}
            </TableHeader>
            <TableBody>
              {table.getRowModel().rows?.length ? (
                table.getRowModel().rows.map((row) => (
                  <TableRow
                    key={row.id}
                    data-state={row.getIsSelected() && "selected"}
                  >
                    {row.getVisibleCells().map((cell) => (
                      <TableCell key={cell.id}>
                        {flexRender(
                          cell.column.columnDef.cell,
                          cell.getContext()
                        )}
                      </TableCell>
                    ))}
                  </TableRow>
                ))
              ) : (
                <TableRow>
                  <TableCell
                    colSpan={columns.length}
                    className="h-24 text-center"
                  >
                    {t("campaigns.edm.templates.noTemplates")}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>

          <div className="flex items-center justify-end space-x-2 py-4">
            <Button
              variant="outline"
              size="sm"
              onClick={() => table.previousPage()}
              disabled={!table.getCanPreviousPage()}
            >
              {t("campaigns.edm.templates.previousPage")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => table.nextPage()}
              disabled={!table.getCanNextPage()}
            >
              {t("campaigns.edm.templates.nextPage")}
            </Button>
          </div>
        </CardContent>
      </Card>

    </div>
  );
}
"use client";

import { useState, useEffect, useCallback } from "react";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { api } from "@/lib/api";
import { getApiErrorMessageZh } from "@/lib/api-errors";
import { toast } from "sonner";
import { Download } from "lucide-react";

interface SurveyStats {
  survey_key: string;
  total: number;
  answers: {
    distribution: Record<string, Record<string, number>>;
    open_texts: Array<{
      user_id: number;
      question: string;
      answer: string;
      created_at: string;
    }>;
  };
}

const SURVEY_OPTIONS = [
  { value: "active_2026q1", label: "2026 Q1 活跃用户问卷" },
  { value: "expired_2026q1", label: "2026 Q1 过期用户问卷" },
];

export default function SurveysPage() {
  const [surveyKey, setSurveyKey] = useState("active_2026q1");
  const [stats, setStats] = useState<SurveyStats | null>(null);
  const [isLoading, setIsLoading] = useState(false);

  const fetchStats = useCallback(async () => {
    setIsLoading(true);
    try {
      const data = await api.getSurveyStats(surveyKey);
      setStats(data);
    } catch (error: unknown) {
      const code = (error as { code?: number })?.code;
      toast.error(code ? getApiErrorMessageZh(code, "加载问卷统计失败") : "加载问卷统计失败");
    } finally {
      setIsLoading(false);
    }
  }, [surveyKey]);

  useEffect(() => {
    fetchStats();
  }, [fetchStats]);

  const formatDate = (dateStr: string) => {
    if (!dateStr) return "-";
    return new Date(dateStr).toLocaleString("zh-CN");
  };

  const getDistributionRows = (questionId: string, dist: Record<string, number>) => {
    const total = Object.values(dist).reduce((sum, v) => sum + v, 0);
    return Object.entries(dist)
      .sort(([, a], [, b]) => b - a)
      .map(([answer, count]) => ({
        answer,
        count,
        percentage: total > 0 ? ((count / total) * 100).toFixed(1) : "0.0",
      }));
  };

  const handleExportCsv = () => {
    if (!stats) return;

    const lines: string[] = [];

    // Header
    lines.push(`问卷: ${stats.survey_key}`);
    lines.push(`总回复数: ${stats.total}`);
    lines.push("");

    // Distribution
    lines.push("=== 选项分布 ===");
    const { distribution, open_texts } = stats.answers;
    for (const [qId, dist] of Object.entries(distribution)) {
      lines.push("");
      lines.push(`问题: ${qId}`);
      lines.push("选项,数量,百分比");
      const rows = getDistributionRows(qId, dist);
      for (const row of rows) {
        lines.push(`${row.answer},${row.count},${row.percentage}%`);
      }
    }

    // Open texts
    if (open_texts && open_texts.length > 0) {
      lines.push("");
      lines.push("=== 开放文本 ===");
      lines.push("用户ID,问题,回答,时间");
      for (const item of open_texts) {
        const answer = item.answer.replace(/"/g, '""');
        lines.push(`${item.user_id},${item.question},"${answer}",${item.created_at}`);
      }
    }

    const blob = new Blob(["\uFEFF" + lines.join("\n")], { type: "text/csv;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `survey-${stats.survey_key}-${new Date().toISOString().slice(0, 10)}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">{"问卷统计"}</h1>
        <p className="text-muted-foreground">{"查看问卷调查回复数据和分布"}</p>
      </div>

      {/* Controls */}
      <div className="flex items-end gap-4 p-4 bg-muted/50 rounded-lg">
        <div className="w-72">
          <label className="text-sm font-medium">{"选择问卷"}</label>
          <Select value={surveyKey} onValueChange={setSurveyKey}>
            <SelectTrigger className="mt-1">
              <SelectValue placeholder="选择问卷" />
            </SelectTrigger>
            <SelectContent>
              {SURVEY_OPTIONS.map((opt) => (
                <SelectItem key={opt.value} value={opt.value}>
                  {opt.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <Button variant="outline" onClick={handleExportCsv} disabled={!stats || stats.total === 0}>
          <Download className="h-4 w-4 mr-2" />
          {"导出 CSV"}
        </Button>
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center h-24">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
        </div>
      ) : stats ? (
        <>
          {/* Summary */}
          <div className="p-4 bg-muted/50 rounded-lg">
            <span className="text-sm text-muted-foreground">{"总回复数: "}</span>
            <span className="text-2xl font-bold">{stats.total}</span>
          </div>

          {/* Distribution tables */}
          {stats.answers.distribution &&
            Object.entries(stats.answers.distribution).map(([questionId, dist]) => {
              const rows = getDistributionRows(questionId, dist);
              return (
                <div key={questionId} className="space-y-2">
                  <h3 className="text-lg font-semibold">{questionId}</h3>
                  <div className="rounded-md border">
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead>{"选项"}</TableHead>
                          <TableHead className="w-24 text-right">{"数量"}</TableHead>
                          <TableHead className="w-32 text-right">{"百分比"}</TableHead>
                          <TableHead className="w-48">{"分布"}</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {rows.map((row) => (
                          <TableRow key={row.answer}>
                            <TableCell>{row.answer}</TableCell>
                            <TableCell className="text-right">{row.count}</TableCell>
                            <TableCell className="text-right">{row.percentage}%</TableCell>
                            <TableCell>
                              <div className="w-full bg-muted rounded-full h-2">
                                <div
                                  className="bg-primary rounded-full h-2"
                                  style={{ width: `${row.percentage}%` }}
                                />
                              </div>
                            </TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </div>
                </div>
              );
            })}

          {/* Open texts */}
          {stats.answers.open_texts && stats.answers.open_texts.length > 0 && (
            <div className="space-y-2">
              <h3 className="text-lg font-semibold">{"开放文本回复"}</h3>
              <div className="rounded-md border">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-24">{"用户 ID"}</TableHead>
                      <TableHead className="w-36">{"问题"}</TableHead>
                      <TableHead>{"回答"}</TableHead>
                      <TableHead className="w-44">{"时间"}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {stats.answers.open_texts.map((item, idx) => (
                      <TableRow key={idx}>
                        <TableCell>
                          <code className="text-xs bg-muted px-1 py-0.5 rounded">
                            {item.user_id}
                          </code>
                        </TableCell>
                        <TableCell>{item.question}</TableCell>
                        <TableCell className="text-sm">{item.answer}</TableCell>
                        <TableCell className="text-sm">{formatDate(item.created_at)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            </div>
          )}
        </>
      ) : (
        <div className="text-center text-muted-foreground py-12">{"暂无数据"}</div>
      )}
    </div>
  );
}

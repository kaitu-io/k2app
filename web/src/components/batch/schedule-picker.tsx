"use client";

import React, { useState, useMemo } from "react";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { ChevronDown, Clock, Calendar } from "lucide-react";
import { cn } from "@/lib/utils";

export interface ScheduleConfig {
  type: "once" | "cron";
  // For once
  executeAt?: number; // milliseconds timestamp
  // For cron
  frequency?: "hourly" | "daily" | "weekly" | "monthly" | "custom";
  time?: string; // HH:mm
  dayOfWeek?: number[]; // 0-6 for weekly (0 = Sunday)
  dayOfMonth?: number[]; // 1-31 for monthly
  cronExpr?: string; // For custom cron
}

interface SchedulePickerProps {
  value: ScheduleConfig;
  onChange: (config: ScheduleConfig) => void;
  className?: string;
}

const WEEKDAYS = [
  { value: 0, label: "周日" },
  { value: 1, label: "周一" },
  { value: 2, label: "周二" },
  { value: 3, label: "周三" },
  { value: 4, label: "周四" },
  { value: 5, label: "周五" },
  { value: 6, label: "周六" },
];

// Parse cron expression to human readable
function describeCron(cronExpr: string): string {
  if (!cronExpr) return "";

  const parts = cronExpr.split(" ");
  if (parts.length !== 5) return "无效的 Cron 表达式";

  const [minute, hour, dayOfMonth, month, dayOfWeek] = parts;

  // Simple patterns
  if (minute === "0" && hour === "*") {
    return "每小时整点执行";
  }

  if (minute !== "*" && hour !== "*" && dayOfMonth === "*" && month === "*") {
    const timeStr = `${hour.padStart(2, "0")}:${minute.padStart(2, "0")}`;
    if (dayOfWeek === "*") {
      return `每天 ${timeStr} 执行`;
    }
    const days = dayOfWeek
      .split(",")
      .map((d) => WEEKDAYS.find((w) => w.value === parseInt(d))?.label || d)
      .join("、");
    return `每周${days} ${timeStr} 执行`;
  }

  if (minute !== "*" && hour !== "*" && dayOfMonth !== "*" && month === "*" && dayOfWeek === "*") {
    const timeStr = `${hour.padStart(2, "0")}:${minute.padStart(2, "0")}`;
    const dates = dayOfMonth.split(",").join("、");
    return `每月 ${dates} 日 ${timeStr} 执行`;
  }

  return cronExpr;
}

// Generate next execution times from cron
function getNextExecutions(cronExpr: string, count: number = 5): Date[] {
  if (!cronExpr) return [];

  const parts = cronExpr.split(" ");
  if (parts.length !== 5) return [];

  const [minute, hour, dayOfMonth, month, dayOfWeek] = parts;
  const results: Date[] = [];
  const now = new Date();
  let current = new Date(now);

  // Simple implementation for common patterns
  for (let i = 0; i < 1000 && results.length < count; i++) {
    current = new Date(current.getTime() + 60 * 1000); // Advance 1 minute

    const matches =
      (minute === "*" || minute.split(",").includes(current.getMinutes().toString())) &&
      (hour === "*" || hour.split(",").includes(current.getHours().toString())) &&
      (dayOfMonth === "*" || dayOfMonth.split(",").includes(current.getDate().toString())) &&
      (month === "*" || month.split(",").includes((current.getMonth() + 1).toString())) &&
      (dayOfWeek === "*" || dayOfWeek.split(",").includes(current.getDay().toString()));

    if (matches) {
      results.push(new Date(current));
      // Skip to next minute boundary to avoid duplicates
      current.setSeconds(0);
      current.setMilliseconds(0);
    }
  }

  return results;
}

// Build cron expression from config
function buildCronExpr(config: ScheduleConfig): string {
  if (config.type !== "cron") return "";

  const [hours, minutes] = (config.time || "02:00").split(":").map(Number);

  switch (config.frequency) {
    case "hourly":
      return "0 * * * *";
    case "daily":
      return `${minutes} ${hours} * * *`;
    case "weekly":
      const days = config.dayOfWeek?.sort().join(",") || "1";
      return `${minutes} ${hours} * * ${days}`;
    case "monthly":
      const dates = config.dayOfMonth?.sort().join(",") || "1";
      return `${minutes} ${hours} ${dates} * *`;
    case "custom":
      return config.cronExpr || "";
    default:
      return "";
  }
}

export function SchedulePicker({ value, onChange, className }: SchedulePickerProps) {
  const [showAdvanced, setShowAdvanced] = useState(value.frequency === "custom");

  // Compute cron expression from current config
  const cronExpr = useMemo(() => {
    if (value.type === "once") return "";
    return buildCronExpr(value);
  }, [value]);

  // Get next execution times
  const nextExecutions = useMemo(() => {
    if (value.type === "once") return [];
    return getNextExecutions(cronExpr, 5);
  }, [value.type, cronExpr]);

  const formatDate = (date: Date) => {
    return date.toLocaleString("zh-CN", {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
    });
  };

  const toggleDayOfWeek = (day: number) => {
    const current = value.dayOfWeek || [];
    const newDays = current.includes(day)
      ? current.filter((d) => d !== day)
      : [...current, day];
    onChange({ ...value, dayOfWeek: newDays });
  };

  const toggleDayOfMonth = (day: number) => {
    const current = value.dayOfMonth || [];
    const newDays = current.includes(day)
      ? current.filter((d) => d !== day)
      : [...current, day];
    onChange({ ...value, dayOfMonth: newDays });
  };

  return (
    <div className={cn("space-y-4", className)}>
      {/* Schedule Type */}
      <RadioGroup
        value={value.type}
        onValueChange={(type: "once" | "cron") => onChange({ ...value, type })}
        className="flex gap-4"
      >
        <div className="flex items-center space-x-2">
          <RadioGroupItem value="once" id="once" />
          <Label htmlFor="once" className="flex items-center gap-2 cursor-pointer">
            <Clock className="h-4 w-4" />
            立即执行
          </Label>
        </div>
        <div className="flex items-center space-x-2">
          <RadioGroupItem value="cron" id="cron" />
          <Label htmlFor="cron" className="flex items-center gap-2 cursor-pointer">
            <Calendar className="h-4 w-4" />
            定时执行
          </Label>
        </div>
      </RadioGroup>

      {/* Cron Configuration */}
      {value.type === "cron" && (
        <div className="space-y-4 p-4 border rounded-lg bg-muted/30">
          {/* Frequency */}
          <div className="space-y-2">
            <Label>执行频率</Label>
            <Select
              value={value.frequency || "daily"}
              onValueChange={(freq) => {
                const newValue: ScheduleConfig = {
                  ...value,
                  frequency: freq as ScheduleConfig["frequency"],
                };
                if (freq === "weekly" && !value.dayOfWeek?.length) {
                  newValue.dayOfWeek = [1]; // Default to Monday
                }
                if (freq === "monthly" && !value.dayOfMonth?.length) {
                  newValue.dayOfMonth = [1]; // Default to 1st
                }
                if (freq === "custom") {
                  setShowAdvanced(true);
                }
                onChange(newValue);
              }}
            >
              <SelectTrigger>
                <SelectValue placeholder="选择执行频率" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="hourly">每小时</SelectItem>
                <SelectItem value="daily">每天</SelectItem>
                <SelectItem value="weekly">每周</SelectItem>
                <SelectItem value="monthly">每月</SelectItem>
                <SelectItem value="custom">自定义 Cron</SelectItem>
              </SelectContent>
            </Select>
          </div>

          {/* Time picker (for daily, weekly, monthly) */}
          {value.frequency !== "hourly" && value.frequency !== "custom" && (
            <div className="space-y-2">
              <Label>执行时间</Label>
              <Input
                type="time"
                value={value.time || "02:00"}
                onChange={(e) => onChange({ ...value, time: e.target.value })}
                className="w-32"
              />
            </div>
          )}

          {/* Day of week selector (for weekly) */}
          {value.frequency === "weekly" && (
            <div className="space-y-2">
              <Label>执行日期</Label>
              <div className="flex flex-wrap gap-2">
                {WEEKDAYS.map((day) => (
                  <Button
                    key={day.value}
                    type="button"
                    variant={value.dayOfWeek?.includes(day.value) ? "default" : "outline"}
                    size="sm"
                    onClick={() => toggleDayOfWeek(day.value)}
                  >
                    {day.label}
                  </Button>
                ))}
              </div>
            </div>
          )}

          {/* Day of month selector (for monthly) */}
          {value.frequency === "monthly" && (
            <div className="space-y-2">
              <Label>执行日期（可多选）</Label>
              <div className="flex flex-wrap gap-1">
                {Array.from({ length: 31 }, (_, i) => i + 1).map((day) => (
                  <Button
                    key={day}
                    type="button"
                    variant={value.dayOfMonth?.includes(day) ? "default" : "outline"}
                    size="sm"
                    className="w-8 h-8 p-0"
                    onClick={() => toggleDayOfMonth(day)}
                  >
                    {day}
                  </Button>
                ))}
              </div>
            </div>
          )}

          {/* Custom cron input */}
          {value.frequency === "custom" && (
            <div className="space-y-2">
              <Label>Cron 表达式</Label>
              <div className="flex gap-2">
                <Input
                  value={value.cronExpr || ""}
                  onChange={(e) => onChange({ ...value, cronExpr: e.target.value })}
                  placeholder="* * * * *"
                  className="font-mono"
                />
              </div>
              <p className="text-xs text-muted-foreground">
                格式: 分钟 小时 日期 月份 星期 (例如: 0 2 * * * = 每天 02:00)
              </p>
            </div>
          )}

          {/* Cron preview */}
          {cronExpr && (
            <Collapsible open={showAdvanced} onOpenChange={setShowAdvanced}>
              <CollapsibleTrigger asChild>
                <Button variant="ghost" size="sm" className="gap-1 w-full justify-between">
                  <span className="text-sm text-muted-foreground">
                    {describeCron(cronExpr)}
                  </span>
                  <ChevronDown
                    className={cn(
                      "h-4 w-4 transition-transform",
                      showAdvanced && "rotate-180"
                    )}
                  />
                </Button>
              </CollapsibleTrigger>
              <CollapsibleContent className="pt-2">
                <div className="text-sm space-y-1">
                  <p className="font-medium">Cron 表达式: <code className="bg-muted px-1 rounded">{cronExpr}</code></p>
                  {nextExecutions.length > 0 && (
                    <div>
                      <p className="font-medium mb-1">接下来的执行时间:</p>
                      <ul className="list-disc list-inside text-muted-foreground">
                        {nextExecutions.map((date, i) => (
                          <li key={i}>{formatDate(date)}</li>
                        ))}
                      </ul>
                    </div>
                  )}
                </div>
              </CollapsibleContent>
            </Collapsible>
          )}
        </div>
      )}
    </div>
  );
}

export default SchedulePicker;

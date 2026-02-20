"use client";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { ExternalLink, Info } from "lucide-react";
import Link from "next/link";

export default function TasksPage() {
  return (
    <div className="container mx-auto p-6">
      <div className="max-w-2xl mx-auto">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Info className="h-5 w-5 text-blue-500" />
              {"任务队列已迁移"}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <p className="text-muted-foreground">
              {"任务队列已迁移至 Asynqmon 进行管理。请使用下方链接访问新的任务管理界面。"}
            </p>
            <div className="flex flex-col sm:flex-row gap-3">
              <Button asChild>
                <Link href="/manager/asynqmon">
                  <ExternalLink className="h-4 w-4 mr-2" />
                  {"前往 Asynqmon"}
                </Link>
              </Button>
              <Button variant="outline" asChild>
                <Link href="/manager/edm/send-logs">
                  {"查看发送日志"}
                </Link>
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

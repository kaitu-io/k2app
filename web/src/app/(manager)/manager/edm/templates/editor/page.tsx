"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState, useCallback } from "react";
import { useSearchParams } from "next/navigation";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/lib/api";
import { toast } from "sonner";
import { ArrowLeft, Save, FileText, Languages, Info, Sparkles } from "lucide-react";
import type { EmailTemplateRequest, EmailTemplateResponse } from "@/lib/api";
import Link from "next/link";

// Frontend-defined template parameters (based on backend contract)
// Reference: server/center/api_admin_edm.go line 245-252
interface EmailTemplateParam {
  key: string;
  name: string;
  description: string;
  category: 'user' | 'subscription' | 'device' | 'system';
  dataType: 'string' | 'number' | 'boolean' | 'date';
  templateVar: string;
  example: string;
  isRequired: boolean;
}

interface EmailTemplateParamGroup {
  category: 'user' | 'subscription' | 'device' | 'system';
  name: string;
  description: string;
  params: EmailTemplateParam[];
}

// Template parameters matching backend implementation
// From: server/center/logic_email_task.go buildEmailTemplateData()
const TEMPLATE_PARAMS: EmailTemplateParamGroup[] = [
  {
    category: 'user',
    name: '用户信息',
    description: '用户基本信息和账户数据',
    params: [
      {
        key: 'user_email',
        name: '用户邮箱',
        description: '用户的登录邮箱地址',
        category: 'user',
        dataType: 'string',
        templateVar: '{{.UserEmail}}',
        example: 'user@example.com',
        isRequired: true,
      },
    ],
  },
  {
    category: 'subscription',
    name: '订阅信息',
    description: '用户订阅状态和有效期',
    params: [
      {
        key: 'expired_at',
        name: '过期日期',
        description: '会员订阅的过期日期 (YYYY-MM-DD)',
        category: 'subscription',
        dataType: 'date',
        templateVar: '{{.ExpiredAt}}',
        example: '2024-12-31',
        isRequired: false,
      },
      {
        key: 'remaining_days',
        name: '剩余天数',
        description: '距离过期的天数',
        category: 'subscription',
        dataType: 'number',
        templateVar: '{{.RemainingDays}}',
        example: '7',
        isRequired: false,
      },
      {
        key: 'is_pro',
        name: '是否Pro用户',
        description: '用户是否为Pro会员',
        category: 'subscription',
        dataType: 'boolean',
        templateVar: '{{.IsPro}}',
        example: 'true',
        isRequired: false,
      },
      {
        key: 'is_expiring_soon',
        name: '是否即将过期',
        description: '是否即将过期 (剩余7天内)',
        category: 'subscription',
        dataType: 'boolean',
        templateVar: '{{.IsExpiringSoon}}',
        example: 'false',
        isRequired: false,
      },
    ],
  },
  {
    category: 'device',
    name: '设备信息',
    description: '用户设备相关信息',
    params: [
      {
        key: 'device_count',
        name: '已用设备数',
        description: '用户当前注册的设备数量',
        category: 'device',
        dataType: 'number',
        templateVar: '{{.DeviceCount}}',
        example: '3',
        isRequired: false,
      },
      {
        key: 'max_devices',
        name: '最大设备数',
        description: '用户允许的最大设备数量',
        category: 'device',
        dataType: 'number',
        templateVar: '{{.MaxDevices}}',
        example: '5',
        isRequired: false,
      },
    ],
  },
];

export default function EmailTemplateEditorPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const templateId = searchParams.get('id');
  const isEditMode = !!templateId;

  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [translating, setTranslating] = useState(false);
  const [translatingLanguage, setTranslatingLanguage] = useState<string>("");
  const [originTemplate, setOriginTemplate] = useState<EmailTemplateResponse | null>(null);
  const [relatedTemplates, setRelatedTemplates] = useState<EmailTemplateResponse[]>([]);
  const [currentLanguage, setCurrentLanguage] = useState<string>("zh-CN");

  // Form state - stores translations for different languages
  const [translations, setTranslations] = useState<Record<string, {
    subject: string;
    content: string;
  }>>({
    "zh-CN": { subject: "", content: "" },
    "zh-TW": { subject: "", content: "" },
    "zh-HK": { subject: "", content: "" },
    "en-US": { subject: "", content: "" },
    "en-GB": { subject: "", content: "" },
    "en-AU": { subject: "", content: "" },
    "ja": { subject: "", content: "" },
  });

  // Base form data (language-independent)
  const [baseFormData, setBaseFormData] = useState({
    name: "",
    description: "",
    isActive: true,
  });

  const languageOptions = [
    { value: "zh-CN", label: "简体中文" },
    { value: "zh-TW", label: "繁体中文" },
    { value: "zh-HK", label: "香港繁体" },
    { value: "en-US", label: "美式英语" },
    { value: "en-GB", label: "英式英语" },
    { value: "en-AU", label: "澳洲英语" },
    { value: "ja", label: "日语" },
  ];

  // Fetch template and all its translations for edit mode
  const fetchTemplate = useCallback(async () => {
    if (!templateId) return;

    try {
      setLoading(true);
      const allTemplates = await api.getEmailTemplates({ limit: 1000 });
      const template = allTemplates.items.find(tmpl => tmpl.id === parseInt(templateId));

      if (!template) {
        toast.error("模板不存在");
        router.push('/manager/edm/templates');
        return;
      }

      // Set base form data
      setBaseFormData({
        name: template.name,
        description: template.description || "",
        isActive: template.isActive,
      });

      // Determine if this is an origin template or a translation
      const isOrigin = !template.originId;
      const originId = isOrigin ? template.id : template.originId;

      // Find origin and all related translations
      const origin = isOrigin ? template : allTemplates.items.find(tmpl => tmpl.id === originId);
      const related = allTemplates.items.filter(tmpl =>
        tmpl.id === originId || tmpl.originId === originId
      );

      setOriginTemplate(origin || null);
      setRelatedTemplates(related);

      // Populate translations from existing templates
      setTranslations(prev => {
        const newTranslations = { ...prev };
        related.forEach(tmpl => {
          newTranslations[tmpl.language] = {
            subject: tmpl.subject,
            content: tmpl.content,
          };
        });
        return newTranslations;
      });
      setCurrentLanguage(template.language);

    } catch (error) {
      toast.error("获取模板失败");
      console.error("Error fetching template:", error);
    } finally {
      setLoading(false);
    }
  }, [templateId, router]);

  // Handle save - simplified to only save modified templates
  const handleSave = async () => {
    // Validate base fields
    if (!baseFormData.name) {
      toast.error("请填写模板名称");
      return;
    }

    // Get current language's content
    const currentTrans = translations[currentLanguage];
    if (!currentTrans.subject.trim() || !currentTrans.content.trim()) {
      toast.error("请填写当前语言的主题和内容");
      return;
    }

    try {
      setSaving(true);

      if (isEditMode && originTemplate) {
        // Update mode: only update templates that have been edited
        // Update all existing translated templates with content
        for (const existingTemplate of relatedTemplates) {
          const lang = existingTemplate.language;
          const trans = translations[lang];

          // Skip if no content (user hasn't filled this language)
          if (!trans.subject.trim() && !trans.content.trim()) continue;

          const templateData: EmailTemplateRequest = {
            name: baseFormData.name,
            language: lang,
            subject: trans.subject,
            content: trans.content,
            description: baseFormData.description,
            isActive: baseFormData.isActive,
            originId: lang === originTemplate.language ? null : originTemplate.id,
          };

          await api.updateEmailTemplate(existingTemplate.id, templateData);
        }

        toast.success("模板更新成功");
      } else {
        // Create mode: only create the origin template
        // User will use "Auto Translate" button to create other languages
        const originData: EmailTemplateRequest = {
          name: baseFormData.name,
          language: currentLanguage,
          subject: currentTrans.subject,
          content: currentTrans.content,
          description: baseFormData.description,
          isActive: baseFormData.isActive,
          originId: null,
        };

        await api.createEmailTemplate(originData);
        toast.success("模板创建成功");
      }

      router.push('/manager/edm/templates');
    } catch (error) {
      toast.error(isEditMode ? "模板更新失败" : "模板创建失败");
      console.error("Error saving template:", error);
    } finally {
      setSaving(false);
    }
  };

  // Auto-translate template using DeepL
  const handleAutoTranslate = async (targetLang: string) => {
    // Must have saved template to translate
    if (!originTemplate) {
      toast.error("请先保存原始模板后再进行翻译");
      return;
    }

    // Check if target language already has content
    const hasContent = translations[targetLang].subject.trim() || translations[targetLang].content.trim();
    if (hasContent) {
      const confirmed = window.confirm(
        `${targetLang} 版本已有内容，确定要用自动翻译覆盖吗？`
      );
      if (!confirmed) return;
    }

    try {
      setTranslating(true);
      setTranslatingLanguage(targetLang);

      const languageLabel = languageOptions.find(l => l.value === targetLang)?.label || targetLang;
      toast.info(`正在使用 DeepL 翻译到 ${languageLabel}...`);

      const translatedTemplate = await api.translateEmailTemplate(originTemplate.id, targetLang);

      // Update translations state
      setTranslations(prev => ({
        ...prev,
        [targetLang]: {
          subject: translatedTemplate.subject,
          content: translatedTemplate.content,
        }
      }));

      // Update related templates list
      setRelatedTemplates(prev => {
        const existing = prev.find(tmpl => tmpl.language === targetLang);
        if (existing) {
          return prev.map(tmpl => tmpl.language === targetLang ? translatedTemplate : tmpl);
        } else {
          return [...prev, translatedTemplate];
        }
      });

      toast.success(`✨ ${languageLabel} 翻译完成！`);
    } catch (error) {
      console.error("Translation error:", error);
      toast.error(`翻译失败: ${error instanceof Error ? error.message : '未知错误'}`);
    } finally {
      setTranslating(false);
      setTranslatingLanguage("");
    }
  };

  // Insert variable into content
  const insertVariableIntoContent = (param: EmailTemplateParam) => {
    const textarea = document.getElementById(`content-${currentLanguage}`) as HTMLTextAreaElement;
    if (textarea) {
      const start = textarea.selectionStart;
      const end = textarea.selectionEnd;
      const text = textarea.value;
      const before = text.substring(0, start);
      const after = text.substring(end, text.length);
      const newText = before + param.templateVar + after;

      setTranslations(prev => ({
        ...prev,
        [currentLanguage]: {
          ...prev[currentLanguage],
          content: newText
        }
      }));

      // Set focus back to textarea and position cursor after inserted variable
      setTimeout(() => {
        textarea.focus();
        textarea.setSelectionRange(start + param.templateVar.length, start + param.templateVar.length);
      }, 0);
    }
  };

  useEffect(() => {
    fetchTemplate();
  }, [fetchTemplate]);

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <div className="text-muted-foreground">{"加载中..."}</div>
      </div>
    );
  }

  // Calculate translation progress
  const getTranslationProgress = () => {
    const completed = languageOptions.filter(lang =>
      translations[lang.value].subject.trim() && translations[lang.value].content.trim()
    ).length;
    return { completed, total: languageOptions.length };
  };

  const progress = getTranslationProgress();

  return (
    <div className="container mx-auto px-4 py-6 max-w-screen-2xl">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center space-x-4">
          <Link href="/manager/edm/templates">
            <Button variant="ghost" size="sm">
              <ArrowLeft className="mr-2 h-4 w-4" />
              {"返回"}
            </Button>
          </Link>
          <div>
            <h1 className="text-2xl font-bold tracking-tight">
              {isEditMode ? "编辑模板" : "创建模板"}
            </h1>
            <p className="text-sm text-muted-foreground">
              {isEditMode ? "编辑邮件模板内容" : "创建新的邮件模板"}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Languages className="h-4 w-4" />
            <span>{progress.completed}{`/`}{progress.total}{` 语言已完成`}</span>
          </div>
          <Button onClick={handleSave} disabled={saving}>
            <Save className="mr-2 h-4 w-4" />
            {saving ? "保存中..." : "保存"}
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Left Column - Template Form */}
        <div className="lg:col-span-2 space-y-6">
          {/* Basic Information */}
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center">
                <FileText className="mr-2 h-5 w-5" />
                {"模板信息"}
              </CardTitle>
              <CardDescription>
                {"设置模板的基本信息"}
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {/* Template Name */}
              <div className="space-y-2">
                <Label htmlFor="name">
                  {"模板名称"} <span className="text-red-500">{`*`}</span>
                </Label>
                <Input
                  id="name"
                  value={baseFormData.name}
                  onChange={(e) => setBaseFormData(prev => ({ ...prev, name: e.target.value }))}
                  placeholder={"输入模板名称"}
                />
              </div>

              {/* Description */}
              <div className="space-y-2">
                <Label htmlFor="description">{"描述"}</Label>
                <Input
                  id="description"
                  value={baseFormData.description}
                  onChange={(e) => setBaseFormData(prev => ({ ...prev, description: e.target.value }))}
                  placeholder={"输入模板描述（可选）"}
                />
              </div>

              {/* Status */}
              <div className="space-y-2">
                <Label htmlFor="status">{"状态"}</Label>
                <Select
                  value={baseFormData.isActive ? "active" : "inactive"}
                  onValueChange={(value) => setBaseFormData(prev => ({ ...prev, isActive: value === "active" }))}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="active">{"启用"}</SelectItem>
                    <SelectItem value="inactive">{"停用"}</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </CardContent>
          </Card>

          {/* Multi-language Content Editor */}
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center">
                <Languages className="mr-2 h-5 w-5" />
                {`多语言内容编辑`}
              </CardTitle>
              <CardDescription>
                {`为不同语言版本编辑邮件主题和内容`}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <Tabs value={currentLanguage} onValueChange={setCurrentLanguage}>
                <TabsList className="grid grid-cols-7 w-full">
                  {languageOptions.map((lang) => (
                    <TabsTrigger
                      key={lang.value}
                      value={lang.value}
                      className="relative"
                    >
                      {lang.label}
                      {translations[lang.value].subject && translations[lang.value].content && (
                        <span className="absolute -top-1 -right-1 h-2 w-2 rounded-full bg-green-500" />
                      )}
                    </TabsTrigger>
                  ))}
                </TabsList>

                {languageOptions.map((lang) => (
                  <TabsContent key={lang.value} value={lang.value} className="space-y-4 mt-4">
                    {/* Auto-translate Button */}
                    {isEditMode && lang.value !== originTemplate?.language && (
                      <div className="flex items-center justify-between p-3 bg-gradient-to-r from-purple-50 to-blue-50 dark:from-purple-950 dark:to-blue-950 rounded-lg border border-purple-200 dark:border-purple-800">
                        <div className="flex items-center gap-2">
                          <Sparkles className="h-4 w-4 text-purple-600 dark:text-purple-400" />
                          <span className="text-sm font-medium text-purple-900 dark:text-purple-100">
                            {`使用 AI 自动翻译此语言版本`}
                          </span>
                        </div>
                        <Button
                          size="sm"
                          variant="default"
                          onClick={() => handleAutoTranslate(lang.value)}
                          disabled={translating || !originTemplate}
                          className="bg-gradient-to-r from-purple-600 to-blue-600 hover:from-purple-700 hover:to-blue-700"
                        >
                          {translating && translatingLanguage === lang.value ? (
                            <>
                              <Sparkles className="mr-2 h-4 w-4 animate-spin" />
                              {`翻译中...`}
                            </>
                          ) : (
                            <>
                              <Sparkles className="mr-2 h-4 w-4" />
                              {`自动翻译`}
                            </>
                          )}
                        </Button>
                      </div>
                    )}

                    {/* Subject */}
                    <div className="space-y-2">
                      <Label htmlFor={`subject-${lang.value}`}>
                        {"邮件主题"} {`(`}{lang.label}{`)`}
                      </Label>
                      <Input
                        id={`subject-${lang.value}`}
                        value={translations[lang.value].subject}
                        onChange={(e) => setTranslations(prev => ({
                          ...prev,
                          [lang.value]: { ...prev[lang.value], subject: e.target.value }
                        }))}
                        placeholder={"输入邮件主题"}
                      />
                      <p className="text-sm text-muted-foreground">
                        {"支持使用模板变量，如 {{.UserEmail}}"}
                      </p>
                    </div>

                    {/* Content */}
                    <div className="space-y-2">
                      <Label htmlFor={`content-${lang.value}`}>
                        {"邮件内容"} {`(`}{lang.label}{`)`}
                      </Label>
                      <textarea
                        id={`content-${lang.value}`}
                        className="w-full min-h-[500px] px-3 py-2 border border-input bg-background text-foreground rounded-md resize-y font-mono text-sm focus:outline-none focus:ring-2 focus:ring-ring focus:border-ring"
                        value={translations[lang.value].content}
                        onChange={(e) => setTranslations(prev => ({
                          ...prev,
                          [lang.value]: { ...prev[lang.value], content: e.target.value }
                        }))}
                        placeholder={"输入邮件内容（HTML格式）"}
                      />
                      <p className="text-sm text-muted-foreground">
                        {"使用右侧的变量卡片插入模板变量"}
                      </p>
                    </div>
                  </TabsContent>
                ))}
              </Tabs>
            </CardContent>
          </Card>
        </div>

        {/* Right Column - Template Variables */}
        <div className="lg:col-span-1">
          <Card className="sticky top-6">
            <CardHeader>
              <CardTitle>{"可用变量"}</CardTitle>
              <CardDescription>
                {"点击变量卡片插入到内容中"}
              </CardDescription>
              <div className="flex items-start gap-2 p-3 mt-2 bg-blue-50 dark:bg-blue-950 rounded-md border border-blue-200 dark:border-blue-800">
                <Info className="h-4 w-4 text-blue-600 dark:text-blue-400 mt-0.5 flex-shrink-0" />
                <p className="text-xs text-blue-700 dark:text-blue-300">
                  {`这些变量由后端自动填充，点击变量卡片可插入到当前语言的邮件内容中`}
                </p>
              </div>
            </CardHeader>
            <CardContent className="p-0">
              <div className="space-y-4 max-h-[600px] overflow-y-auto p-6">
                {TEMPLATE_PARAMS.map((group) => (
                  <div key={group.category}>
                    <div className="mb-3">
                      <h4 className="font-semibold text-sm">{group.name}</h4>
                      <p className="text-xs text-muted-foreground">{group.description}</p>
                    </div>
                    <div className="space-y-2">
                      {group.params.map((param) => (
                        <div
                          key={param.key}
                          className="border border-border rounded-lg p-3 hover:bg-accent hover:text-accent-foreground cursor-pointer transition-colors group"
                          onClick={() => insertVariableIntoContent(param)}
                        >
                          <div className="flex items-center justify-between mb-1">
                            <Badge variant="outline" className="text-xs">
                              {param.dataType}
                            </Badge>
                            {param.isRequired && (
                              <Badge variant="default" className="text-xs">
                                {"必填"}
                              </Badge>
                            )}
                          </div>
                          <div className="font-medium text-sm mb-1">{param.name}</div>
                          <div className="text-xs text-muted-foreground mb-2">{param.description}</div>
                          <div className="font-mono text-xs bg-muted px-2 py-1 rounded group-hover:bg-accent group-hover:text-accent-foreground transition-colors">
                            {param.templateVar}
                          </div>
                          {param.example && (
                            <div className="text-xs text-muted-foreground mt-1">
                              {"示例"}{": "}{param.example}
                            </div>
                          )}
                        </div>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}

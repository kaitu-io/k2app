"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { api, CloudAccount, CloudRegion, CloudPlan, CloudImage } from "@/lib/api";
import { toast } from "sonner";
import { ArrowLeft, Loader2, Server, HardDrive, Cpu, MemoryStick, Terminal, Copy, Check } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

// Provider display names
const providerNames: Record<string, string> = {
  bandwagon: "搬瓦工",
  aws_lightsail: "AWS Lightsail",
  alibaba_swas: "阿里云轻量(国际)",
  aliyun_swas: "阿里云轻量(国内)",
  tencent_lighthouse: "腾讯云轻量(国际)",
  qcloud_lighthouse: "腾讯云轻量(国内)",
  ssh_standalone: "独立主机 (SSH)",
};

export default function CreateCloudInstancePage() {
  const router = useRouter();

  const [accounts, setAccounts] = useState<CloudAccount[]>([]);
  const [regions, setRegions] = useState<CloudRegion[]>([]);
  const [plans, setPlans] = useState<CloudPlan[]>([]);
  const [images, setImages] = useState<CloudImage[]>([]);

  const [selectedAccount, setSelectedAccount] = useState<string>("");
  const [selectedRegion, setSelectedRegion] = useState<string>("");
  const [selectedPlan, setSelectedPlan] = useState<string>("");
  const [selectedImage, setSelectedImage] = useState<string>("");
  const [instanceName, setInstanceName] = useState<string>("");

  // SSH Standalone mode
  const [isSSHStandalone, setIsSSHStandalone] = useState(false);
  const [copied, setCopied] = useState(false);

  const [isLoadingAccounts, setIsLoadingAccounts] = useState(true);
  const [isLoadingRegions, setIsLoadingRegions] = useState(false);
  const [isLoadingPlans, setIsLoadingPlans] = useState(false);
  const [isLoadingImages, setIsLoadingImages] = useState(false);
  const [isCreating, setIsCreating] = useState(false);

  // Fetch accounts on mount
  useEffect(() => {
    const fetchAccounts = async () => {
      try {
        const response = await api.listCloudAccounts();
        // Filter out bandwagon accounts (can't create instances)
        const createableAccounts = (response.items || []).filter(
          acc => acc.provider !== "bandwagon"
        );
        setAccounts(createableAccounts);
      } catch (error) {
        console.error("Failed to fetch accounts:", error);
        toast.error("获取账号列表失败");
      } finally {
        setIsLoadingAccounts(false);
      }
    };
    fetchAccounts();
  }, []);

  // Fetch regions when account changes
  useEffect(() => {
    if (!selectedAccount) {
      setRegions([]);
      return;
    }

    const fetchRegions = async () => {
      setIsLoadingRegions(true);
      setSelectedRegion("");
      setPlans([]);
      setImages([]);
      try {
        const response = await api.listCloudRegions({ account: selectedAccount });
        setRegions(response.items || []);
      } catch (error) {
        console.error("Failed to fetch regions:", error);
        toast.error("获取区域列表失败");
      } finally {
        setIsLoadingRegions(false);
      }
    };
    fetchRegions();
  }, [selectedAccount]);

  // Fetch plans and images when region changes
  useEffect(() => {
    if (!selectedAccount || !selectedRegion) {
      setPlans([]);
      setImages([]);
      return;
    }

    const fetchPlansAndImages = async () => {
      setIsLoadingPlans(true);
      setIsLoadingImages(true);
      setSelectedPlan("");
      setSelectedImage("");
      try {
        const [plansRes, imagesRes] = await Promise.all([
          api.listCloudPlans({ account: selectedAccount, region: selectedRegion }),
          api.listCloudImages({ account: selectedAccount, region: selectedRegion }),
        ]);
        setPlans(plansRes.items || []);
        setImages(imagesRes.items || []);
      } catch (error) {
        console.error("Failed to fetch plans/images:", error);
        toast.error("获取套餐或镜像列表失败");
      } finally {
        setIsLoadingPlans(false);
        setIsLoadingImages(false);
      }
    };
    fetchPlansAndImages();
  }, [selectedAccount, selectedRegion]);

  const handleCreate = async () => {
    if (!selectedAccount || !selectedRegion || !selectedPlan || !selectedImage || !instanceName) {
      toast.error("请填写所有必填项");
      return;
    }

    setIsCreating(true);
    try {
      const response = await api.createCloudInstance({
        account_name: selectedAccount,
        region: selectedRegion,
        plan: selectedPlan,
        image_id: selectedImage,
        name: instanceName,
      });
      toast.success(`创建任务已提交，任务 ID: ${response.task_id}`);
      router.push("/manager/cloud");
    } catch (error) {
      console.error("Failed to create instance:", error);
      toast.error("创建实例失败");
    } finally {
      setIsCreating(false);
    }
  };

  const selectedAccountObj = accounts.find(a => a.name === selectedAccount);
  const selectedPlanObj = plans.find(p => p.id === selectedPlan);
  const selectedImageObj = images.find(i => i.id === selectedImage);

  // Install script command
  const installCommand = `curl -fsSL https://k2.52j.me/slave/init-node.sh | sudo bash -s -- \\
  --secret "YOUR_NODE_SECRET" \\
  --name "your-node-name" \\
  --region "your-region"`;

  const handleCopyCommand = async () => {
    try {
      await navigator.clipboard.writeText(installCommand);
      setCopied(true);
      toast.success("已复制到剪贴板");
      setTimeout(() => setCopied(false), 2000);
    } catch {
      toast.error("复制失败");
    }
  };

  const handleSelectSSHStandalone = () => {
    setIsSSHStandalone(true);
    setSelectedAccount("");
    setSelectedRegion("");
    setSelectedPlan("");
    setSelectedImage("");
    setInstanceName("");
  };

  const handleSelectCloudProvider = () => {
    setIsSSHStandalone(false);
  };

  return (
    <div className="container mx-auto py-10">
      <Button
        variant="ghost"
        onClick={() => router.push("/manager/cloud")}
        className="mb-4"
      >
        <ArrowLeft className="h-4 w-4 mr-2" />
        返回列表
      </Button>

      <div className="mb-6">
        <h1 className="text-3xl font-bold">创建云实例</h1>
        <p className="text-muted-foreground">在云服务商上创建新的 VPS 实例</p>
      </div>

      <div className="grid gap-6">
        {/* Deployment Method Selection */}
        <Card>
          <CardHeader>
            <CardTitle>部署方式</CardTitle>
            <CardDescription>选择如何部署节点</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div
                className={`p-4 rounded-lg border cursor-pointer transition-colors ${
                  !isSSHStandalone
                    ? "border-primary bg-primary/5"
                    : "border-border hover:border-primary/50"
                }`}
                onClick={handleSelectCloudProvider}
              >
                <div className="flex items-center gap-2 font-medium">
                  <Server className="h-5 w-5" />
                  云服务商 API
                </div>
                <p className="text-sm text-muted-foreground mt-2">
                  通过云服务商 API 自动创建和配置实例
                </p>
              </div>
              <div
                className={`p-4 rounded-lg border cursor-pointer transition-colors ${
                  isSSHStandalone
                    ? "border-primary bg-primary/5"
                    : "border-border hover:border-primary/50"
                }`}
                onClick={handleSelectSSHStandalone}
              >
                <div className="flex items-center gap-2 font-medium">
                  <Terminal className="h-5 w-5" />
                  独立主机 (SSH)
                </div>
                <p className="text-sm text-muted-foreground mt-2">
                  在已有服务器上运行安装脚本
                </p>
              </div>
            </div>
          </CardContent>
        </Card>

        {/* SSH Standalone Mode */}
        {isSSHStandalone && (
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Terminal className="h-5 w-5" />
                一键安装脚本
              </CardTitle>
              <CardDescription>
                在您的服务器上以 root 身份运行以下命令
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <Alert>
                <Terminal className="h-4 w-4" />
                <AlertTitle>系统要求</AlertTitle>
                <AlertDescription>
                  Ubuntu 22.04/24.04 LTS，需要 root 权限
                </AlertDescription>
              </Alert>

              <div className="relative">
                <pre className="bg-muted p-4 rounded-lg text-sm overflow-x-auto font-mono">
                  {installCommand}
                </pre>
                <Button
                  variant="outline"
                  size="sm"
                  className="absolute top-2 right-2"
                  onClick={handleCopyCommand}
                >
                  {copied ? (
                    <Check className="h-4 w-4" />
                  ) : (
                    <Copy className="h-4 w-4" />
                  )}
                </Button>
              </div>

              <div className="text-sm text-muted-foreground space-y-2">
                <p><strong>参数说明：</strong></p>
                <ul className="list-disc list-inside space-y-1 ml-2">
                  <li><code className="bg-muted px-1 rounded">--secret</code>: 节点密钥（必填，从节点管理页面获取）</li>
                  <li><code className="bg-muted px-1 rounded">--name</code>: 节点名称（如 hk-01）</li>
                  <li><code className="bg-muted px-1 rounded">--region</code>: 节点区域（如 hongkong）</li>
                </ul>
              </div>

              <Alert>
                <AlertTitle>脚本功能</AlertTitle>
                <AlertDescription className="mt-2">
                  <ul className="list-disc list-inside space-y-1">
                    <li>创建 ubuntu 用户并配置 SSH 公钥</li>
                    <li>安装 Docker 及 docker-compose</li>
                    <li>自动部署 K2 节点服务</li>
                    <li>配置系统优化（swap、时区等）</li>
                  </ul>
                </AlertDescription>
              </Alert>
            </CardContent>
          </Card>
        )}

        {/* Account Selection - only show for cloud provider mode */}
        {!isSSHStandalone && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Server className="h-5 w-5" />
              选择账号
            </CardTitle>
            <CardDescription>选择要使用的云服务商账号</CardDescription>
          </CardHeader>
          <CardContent>
            {isLoadingAccounts ? (
              <div className="flex items-center gap-2 text-muted-foreground">
                <Loader2 className="h-4 w-4 animate-spin" />
                加载账号列表...
              </div>
            ) : accounts.length === 0 ? (
              <p className="text-muted-foreground">没有可用的账号（搬瓦工不支持创建实例）</p>
            ) : (
              <Select value={selectedAccount} onValueChange={setSelectedAccount}>
                <SelectTrigger className="w-full">
                  <SelectValue placeholder="选择账号" />
                </SelectTrigger>
                <SelectContent>
                  {accounts.map((acc) => (
                    <SelectItem key={acc.name} value={acc.name}>
                      {acc.name} ({providerNames[acc.provider] || acc.provider})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </CardContent>
        </Card>
        )}

        {/* Region Selection */}
        {selectedAccount && (
          <Card>
            <CardHeader>
              <CardTitle>选择区域</CardTitle>
              <CardDescription>
                服务商: {providerNames[selectedAccountObj?.provider || ""] || selectedAccountObj?.provider}
              </CardDescription>
            </CardHeader>
            <CardContent>
              {isLoadingRegions ? (
                <div className="flex items-center gap-2 text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  加载区域列表...
                </div>
              ) : regions.length === 0 ? (
                <p className="text-muted-foreground">没有可用的区域</p>
              ) : (
                <Select value={selectedRegion} onValueChange={setSelectedRegion}>
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder="选择区域" />
                  </SelectTrigger>
                  <SelectContent>
                    {regions.map((region) => (
                      <SelectItem key={region.slug || region.providerId} value={region.slug || region.providerId}>
                        {region.nameZh || region.nameEn} ({region.country})
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </CardContent>
          </Card>
        )}

        {/* Plan Selection */}
        {selectedRegion && (
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Cpu className="h-5 w-5" />
                选择套餐
              </CardTitle>
              <CardDescription>选择实例配置</CardDescription>
            </CardHeader>
            <CardContent>
              {isLoadingPlans ? (
                <div className="flex items-center gap-2 text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  加载套餐列表...
                </div>
              ) : plans.length === 0 ? (
                <p className="text-muted-foreground">没有可用的套餐</p>
              ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  {plans.map((plan) => (
                    <div
                      key={plan.id}
                      className={`p-4 rounded-lg border cursor-pointer transition-colors ${
                        selectedPlan === plan.id
                          ? "border-primary bg-primary/5"
                          : "border-border hover:border-primary/50"
                      }`}
                      onClick={() => setSelectedPlan(plan.id)}
                    >
                      <div className="font-medium">{plan.name}</div>
                      <div className="text-sm text-muted-foreground mt-2 space-y-1">
                        <div className="flex items-center gap-2">
                          <Cpu className="h-3 w-3" />
                          {plan.cpu} vCPU
                        </div>
                        <div className="flex items-center gap-2">
                          <MemoryStick className="h-3 w-3" />
                          {plan.memoryMB >= 1024 ? `${(plan.memoryMB / 1024).toFixed(1)} GB` : `${plan.memoryMB} MB`} 内存
                        </div>
                        <div className="flex items-center gap-2">
                          <HardDrive className="h-3 w-3" />
                          {plan.storageGB} GB SSD
                        </div>
                      </div>
                      <div className="mt-2 text-sm">
                        流量: {plan.transferTB != null && !isNaN(plan.transferTB)
                          ? (plan.transferTB >= 1 ? `${plan.transferTB.toFixed(2)} TB` : `${(plan.transferTB * 1024).toFixed(0)} GB`)
                          : '-'}/月
                      </div>
                      <div className="mt-2 font-semibold text-primary">
                        ${plan.priceMonthly.toFixed(2)}/月
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        )}

        {/* Image Selection */}
        {selectedPlan && (
          <Card>
            <CardHeader>
              <CardTitle>选择操作系统</CardTitle>
              <CardDescription>选择预装的操作系统镜像</CardDescription>
            </CardHeader>
            <CardContent>
              {isLoadingImages ? (
                <div className="flex items-center gap-2 text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  加载镜像列表...
                </div>
              ) : images.length === 0 ? (
                <p className="text-muted-foreground">没有可用的镜像</p>
              ) : (
                <Select value={selectedImage} onValueChange={setSelectedImage}>
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder="选择操作系统" />
                  </SelectTrigger>
                  <SelectContent>
                    {images.map((image) => (
                      <SelectItem key={image.id} value={image.id}>
                        {image.name}
                        {image.description && ` - ${image.description}`}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </CardContent>
          </Card>
        )}

        {/* Instance Name */}
        {selectedImage && (
          <Card>
            <CardHeader>
              <CardTitle>实例名称</CardTitle>
              <CardDescription>为新实例设置一个名称</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-2">
                <Label htmlFor="name">名称</Label>
                <Input
                  id="name"
                  placeholder="例如: my-server-01"
                  value={instanceName}
                  onChange={(e) => setInstanceName(e.target.value)}
                />
              </div>
            </CardContent>
          </Card>
        )}

        {/* Summary and Create */}
        {instanceName && (
          <Card>
            <CardHeader>
              <CardTitle>确认配置</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="space-y-2 text-sm">
                <div className="flex justify-between">
                  <span className="text-muted-foreground">账号:</span>
                  <span>{selectedAccount}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">区域:</span>
                  <span>{regions.find(r => r.slug === selectedRegion || r.providerId === selectedRegion)?.nameZh || selectedRegion}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">套餐:</span>
                  <span>{selectedPlanObj?.name}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">系统:</span>
                  <span>{selectedImageObj?.name}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">名称:</span>
                  <span>{instanceName}</span>
                </div>
                <div className="flex justify-between font-semibold pt-2 border-t">
                  <span>预估费用:</span>
                  <span className="text-primary">${selectedPlanObj?.priceMonthly.toFixed(2)}/月</span>
                </div>
              </div>
              <Button
                className="w-full mt-4"
                onClick={handleCreate}
                disabled={isCreating}
              >
                {isCreating ? (
                  <>
                    <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                    创建中...
                  </>
                ) : (
                  "创建实例"
                )}
              </Button>
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  );
}

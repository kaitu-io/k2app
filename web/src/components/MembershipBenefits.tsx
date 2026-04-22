"use client";

import { useTranslations } from "next-intl";
import { Card, CardContent } from "@/components/ui/card";
import {
  Smartphone,
  Globe,
  Rocket,
  RefreshCw,
  Headphones,
  type LucideIcon,
} from "lucide-react";

const DEFAULT_DEVICE_COUNT = 5;

interface BenefitRow {
  key: "multiDevice" | "globalNodes" | "zeroMaintenance" | "continuousOptimization" | "prioritySupport";
  icon: LucideIcon;
  iconClass: string;
  count?: number;
}

const BENEFITS: BenefitRow[] = [
  { key: "multiDevice", icon: Smartphone, iconClass: "text-blue-500", count: DEFAULT_DEVICE_COUNT },
  { key: "globalNodes", icon: Globe, iconClass: "text-green-500" },
  { key: "zeroMaintenance", icon: Rocket, iconClass: "text-orange-500" },
  { key: "continuousOptimization", icon: RefreshCw, iconClass: "text-violet-500" },
  { key: "prioritySupport", icon: Headphones, iconClass: "text-fuchsia-500" },
];

export default function MembershipBenefits() {
  const t = useTranslations();

  return (
    <Card>
      <CardContent className="p-4 sm:p-6">
        <h2 className="text-lg sm:text-xl font-bold text-foreground mb-3 sm:mb-4">
          {t("purchase.purchase.memberBenefits")}
        </h2>
        <ul className="grid grid-cols-1 sm:grid-cols-2 gap-2 sm:gap-3">
          {BENEFITS.map(({ key, icon: Icon, iconClass, count }) => {
            const titleKey = `purchase.purchase.features.${key}`;
            const descKey = `purchase.purchase.features.${key}Desc`;
            const title = count !== undefined ? t(titleKey, { count }) : t(titleKey);

            return (
              <li
                key={key}
                className="flex items-start gap-3 rounded-lg bg-muted/50 px-3 py-2.5"
              >
                <Icon className={`w-5 h-5 mt-0.5 flex-shrink-0 ${iconClass}`} />
                <div className="min-w-0">
                  <p className="text-sm sm:text-base font-semibold text-foreground leading-tight">
                    {title}
                  </p>
                  <p className="text-xs sm:text-sm text-muted-foreground mt-0.5 leading-snug">
                    {t(descKey)}
                  </p>
                </div>
              </li>
            );
          })}
        </ul>
      </CardContent>
    </Card>
  );
}

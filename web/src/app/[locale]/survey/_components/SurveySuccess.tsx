"use client";

import { useTranslations } from "next-intl";
import { CheckCircle2 } from "lucide-react";

interface SurveySuccessProps {
  rewardDays: number;
  newExpiredAt: number;
}

export default function SurveySuccess({ rewardDays, newExpiredAt }: SurveySuccessProps) {
  const t = useTranslations();

  const expiryDate = new Date(newExpiredAt * 1000).toLocaleDateString(undefined, {
    year: "numeric",
    month: "long",
    day: "numeric",
  });

  return (
    <div className="flex flex-col items-center text-center space-y-4 py-8">
      <CheckCircle2 className="h-16 w-16 text-green-500" />
      <h2 className="text-2xl font-bold">{t("survey.success_title")}</h2>
      <p className="text-muted-foreground">
        {t("survey.success_reward", { days: rewardDays })}
      </p>
      <p className="text-sm text-muted-foreground">
        {t("survey.success_new_expiry", { date: expiryDate })}
      </p>
    </div>
  );
}

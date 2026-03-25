"use client";

import { useState, useEffect } from "react";
import { useParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { useAuth } from "@/contexts/AuthContext";
import { redirectToLogin } from "@/lib/auth";
import { api, ApiError, ErrorCode } from "@/lib/api";
import { surveys } from "../_components/surveyConfig";
import SurveyForm from "../_components/SurveyForm";
import SurveySuccess from "../_components/SurveySuccess";
import Image from "next/image";
import { Link } from "@/i18n/routing";

type PageState =
  | { kind: "loading" }
  | { kind: "not_found" }
  | { kind: "already_submitted" }
  | { kind: "form" }
  | { kind: "success"; rewardDays: number; newExpiredAt: number }
  | { kind: "error"; message: string };

export default function SurveyPage() {
  const params = useParams();
  const surveyKey = params.surveyKey as string;
  const t = useTranslations();
  const { isAuthenticated, isAuthLoading } = useAuth();
  const [pageState, setPageState] = useState<PageState>({ kind: "loading" });
  const [isSubmitting, setIsSubmitting] = useState(false);

  const config = surveys[surveyKey];

  // Redirect to login if not authenticated
  useEffect(() => {
    if (isAuthLoading) return;
    if (!isAuthenticated) {
      redirectToLogin();
    }
  }, [isAuthenticated, isAuthLoading]);

  // Check survey status once authenticated
  useEffect(() => {
    if (isAuthLoading || !isAuthenticated) return;

    if (!config) {
      setPageState({ kind: "not_found" });
      return;
    }

    const checkStatus = async () => {
      try {
        const status = await api.getSurveyStatus(surveyKey);
        if (status.submitted) {
          setPageState({ kind: "already_submitted" });
        } else {
          setPageState({ kind: "form" });
        }
      } catch {
        // If status check fails, still show form — submit will catch errors
        setPageState({ kind: "form" });
      }
    };
    checkStatus();
  }, [isAuthLoading, isAuthenticated, surveyKey, config]);

  const handleSubmit = async (answers: Record<string, string>) => {
    setIsSubmitting(true);
    try {
      const result = await api.submitSurvey(surveyKey, answers);
      setPageState({
        kind: "success",
        rewardDays: result.reward_days,
        newExpiredAt: result.new_expired_at,
      });
    } catch (err) {
      if (err instanceof ApiError && err.code === ErrorCode.Conflict) {
        setPageState({ kind: "already_submitted" });
      } else {
        setPageState({ kind: "error", message: t("errors.unknown") });
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  // Show nothing while auth is loading
  if (isAuthLoading || (!isAuthenticated && !isAuthLoading)) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-background">
      <div className="mx-auto max-w-lg px-4 py-8">
        {/* Logo */}
        <div className="flex justify-center mb-8">
          <Link href="/">
            <Image
              src="/kaitu-icon.png"
              alt="Kaitu"
              width={48}
              height={48}
              className="rounded-lg"
            />
          </Link>
        </div>

        {pageState.kind === "loading" && (
          <div className="flex justify-center py-16">
            <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
          </div>
        )}

        {pageState.kind === "not_found" && (
          <div className="text-center py-16">
            <h1 className="text-2xl font-bold mb-2">{t("survey.not_found")}</h1>
          </div>
        )}

        {pageState.kind === "already_submitted" && (
          <div className="text-center py-16 space-y-2">
            <h1 className="text-2xl font-bold">{t("survey.already_submitted")}</h1>
            <p className="text-muted-foreground">{t("survey.already_submitted_desc")}</p>
          </div>
        )}

        {pageState.kind === "error" && (
          <div className="text-center py-16">
            <p className="text-destructive">{pageState.message}</p>
          </div>
        )}

        {pageState.kind === "form" && config && (
          <>
            <div className="text-center mb-8">
              <h1 className="text-2xl font-bold mb-2">{t("survey.title")}</h1>
              <p className="text-muted-foreground">{t(config.subtitleKey)}</p>
            </div>
            <SurveyForm
              config={config}
              onSubmit={handleSubmit}
              isSubmitting={isSubmitting}
            />
          </>
        )}

        {pageState.kind === "success" && (
          <SurveySuccess
            rewardDays={pageState.rewardDays}
            newExpiredAt={pageState.newExpiredAt}
          />
        )}
      </div>
    </div>
  );
}

"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState } from "react";
import { useTranslations } from "next-intl";
import { api, type Delegate, ApiError } from "@/lib/api";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { CircleDashed, UserX, Mail, AlertTriangle } from "lucide-react";
import { toast } from "sonner";

/**
 * Delegate Management Page
 *
 * Allows users to view and manage their payer (delegate) relationship.
 * Users who have been added as members by another user can see the payer's
 * information and choose to reject the payment delegation.
 */
export default function DelegatePage() {
  const t = useTranslations();
  const [delegate, setDelegate] = useState<Delegate | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [rejecting, setRejecting] = useState(false);

  useEffect(() => {
    loadDelegate();
  }, []);

  const loadDelegate = async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await api.getDelegate({ autoRedirectToAuth: false });
      setDelegate(data);
    } catch (err) {
      if (err instanceof ApiError && err.code === 404) {
        // No delegate found - this is normal
        setDelegate(null);
      } else {
        console.error("Failed to load delegate:", err);
        setError(err instanceof Error ? err.message : "Failed to load delegate");
      }
    } finally {
      setLoading(false);
    }
  };

  const handleReject = async () => {
    if (!delegate) return;

    try {
      setRejecting(true);
      await api.rejectDelegate();

      toast.success(t("admin.account.delegate.rejectSuccess.description"));

      // Reload to show updated state
      await loadDelegate();
    } catch (err) {
      console.error("Failed to reject delegate:", err);
      toast.error(err instanceof Error ? err.message : t("admin.account.delegate.rejectFailed"));
    } finally {
      setRejecting(false);
    }
  };

  if (loading) {
    return (
      <div className="flex min-h-[400px] items-center justify-center">
        <CircleDashed className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="rounded-lg border border-destructive bg-destructive/10 p-4">
        <div className="flex items-center gap-2">
          <AlertTriangle className="h-4 w-4 text-destructive" />
          <p className="text-sm text-destructive">{error}</p>
        </div>
      </div>
    );
  }

  if (!delegate) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{t("admin.account.delegate.title")}</CardTitle>
          <CardDescription>{t("admin.account.delegate.description")}</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="rounded-lg border border-border bg-muted/50 p-4">
            <p className="text-sm text-muted-foreground">{t("admin.account.delegate.noDelegate")}</p>
          </div>
        </CardContent>
      </Card>
    );
  }

  // Get the primary email
  const emailIdentify = delegate.loginIdentifies.find((id) => id.type === "email");

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("admin.account.delegate.title")}</CardTitle>
        <CardDescription>{t("admin.account.delegate.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {/* Delegate Information */}
        <div className="space-y-4">
          <div className="flex items-start gap-3 p-4 border rounded-lg">
            <Mail className="h-5 w-5 text-muted-foreground mt-0.5" />
            <div className="flex-1 min-w-0">
              <p className="text-sm font-medium">{t("admin.account.delegate.payerEmail")}</p>
              <p className="text-sm text-muted-foreground break-all">
                {emailIdentify?.value || t("common.common.notAvailable")}
              </p>
            </div>
          </div>

          <div className="rounded-lg border border-border bg-muted/50 p-4">
            <p className="text-sm text-muted-foreground">
              {t("admin.account.delegate.info")}
            </p>
          </div>
        </div>

        {/* Reject Button */}
        <div className="pt-4 border-t">
          <div className="space-y-4">
            <div className="rounded-lg border border-destructive bg-destructive/10 p-4">
              <div className="flex items-center gap-2">
                <AlertTriangle className="h-4 w-4 text-destructive" />
                <p className="text-sm text-destructive">
                  {t("admin.account.delegate.rejectWarning")}
                </p>
              </div>
            </div>

            <Button
              variant="destructive"
              onClick={handleReject}
              disabled={rejecting}
              className="w-full sm:w-auto"
            >
              {rejecting ? (
                <>
                  <CircleDashed className="mr-2 h-4 w-4 animate-spin" />
                  {t("common.common.processing")}
                </>
              ) : (
                <>
                  <UserX className="mr-2 h-4 w-4" />
                  {t("admin.account.delegate.rejectButton")}
                </>
              )}
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

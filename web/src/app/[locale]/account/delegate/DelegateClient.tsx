"use client";

import { useState, useEffect } from "react";
import { useTranslations } from "next-intl";
import { useSearchParams } from "next/navigation";
import { useRouter } from "@/i18n/routing";
import { toast } from "sonner";
import { api, type DelegateInfo } from "@/lib/api";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Loader2 } from "lucide-react";

export default function DelegateClient() {
  const t = useTranslations();
  const router = useRouter();
  const searchParams = useSearchParams();
  const returnTo = searchParams.get("returnTo");

  const [loading, setLoading] = useState(true);
  const [delegate, setDelegate] = useState<DelegateInfo | null>(null);
  const [editing, setEditing] = useState(false);
  const [email, setEmail] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    (async () => {
      try {
        const data = await api.getDelegate({ autoRedirectToAuth: false });
        setDelegate(data);
        setEditing(!data);
      } catch (err) {
        console.error("Failed to load delegate:", err);
        setDelegate(null);
        setEditing(true);
      } finally {
        setLoading(false);
      }
    })();
  }, []);

  const onSave = async () => {
    const trimmed = email.trim();
    if (!trimmed) return;
    setSaving(true);
    try {
      const result = await api.setDelegate(trimmed);
      setDelegate(result);
      setEditing(false);
      setEmail("");
      toast.success(t("account.account.delegate.savedToast"));
      if (returnTo) {
        router.push(returnTo);
      }
    } catch (err) {
      console.error("Failed to save delegate:", err);
      toast.error(err instanceof Error ? err.message : "Error");
    } finally {
      setSaving(false);
    }
  };

  const onRemove = async () => {
    if (typeof window !== "undefined" && !window.confirm(t("account.account.delegate.removeConfirm"))) {
      return;
    }
    setSaving(true);
    try {
      await api.removeDelegate();
      setDelegate(null);
      setEditing(true);
    } catch (err) {
      console.error("Failed to remove delegate:", err);
      toast.error(err instanceof Error ? err.message : "Error");
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex min-h-[200px] items-center justify-center">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  const showForm = !delegate || editing;

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("account.account.delegate.pageTitle")}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {showForm ? (
          <>
            <p className="text-sm text-muted-foreground">
              {t("account.account.delegate.emptyDescription")}
            </p>
            <div className="space-y-2">
              <Label htmlFor="delegate-email">
                {t("account.account.delegate.emailLabel")}
              </Label>
              <Input
                id="delegate-email"
                type="email"
                placeholder={t("account.account.delegate.emailPlaceholder")}
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                disabled={saving}
              />
            </div>
            <div className="flex gap-2">
              <Button onClick={onSave} disabled={saving || !email.trim()}>
                {saving && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                {t("account.account.delegate.saveButton")}
              </Button>
              {delegate && (
                <Button
                  variant="ghost"
                  onClick={() => {
                    setEditing(false);
                    setEmail("");
                  }}
                  disabled={saving}
                >
                  {t("account.account.delegate.cancelButton")}
                </Button>
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              {t("account.account.delegate.emptyHint")}
            </p>
          </>
        ) : (
          <>
            <div className="rounded-md border bg-muted/50 p-4">
              <div className="text-xs text-muted-foreground mb-1">
                {t("account.account.delegate.currentTitle")}
              </div>
              <div className="text-lg font-bold break-all">{delegate.email}</div>
              <div className="text-xs text-muted-foreground mt-1">
                {t("account.account.delegate.setAtLabel", {
                  date: new Date(delegate.setAt * 1000).toLocaleDateString(),
                })}
              </div>
            </div>
            <div className="flex gap-2">
              <Button
                variant="outline"
                onClick={() => setEditing(true)}
                disabled={saving}
              >
                {t("account.account.delegate.modifyButton")}
              </Button>
              <Button
                variant="destructive"
                onClick={onRemove}
                disabled={saving}
              >
                {saving && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                {t("account.account.delegate.removeButton")}
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              {t("account.account.delegate.currentHint", { email: delegate.email })}
            </p>
          </>
        )}
      </CardContent>
    </Card>
  );
}

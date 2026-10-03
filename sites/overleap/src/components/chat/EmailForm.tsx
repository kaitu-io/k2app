'use client';

import { useState, type FormEvent } from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

export interface EmailFormLabels {
  prompt: string;
  placeholder: string;
  submit: string;
  invalid: string;
  failed: string;
}

// 与服务端同量级的格式检查：一个 @、域名带点、无空白；真正的校验在服务端。
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/** 转人工后客服迟迟未接入时请访客留邮箱。`onSubmit` 抛错 = 提交失败（保留输入，可重试）。 */
export default function EmailForm({
  labels,
  onSubmit,
}: {
  labels: EmailFormLabels;
  onSubmit: (email: string) => Promise<void>;
}) {
  const [value, setValue] = useState('');
  const [error, setError] = useState<'invalid' | 'failed' | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    const email = value.trim();
    if (email.length > 254 || !EMAIL_RE.test(email)) {
      setError('invalid');
      return;
    }
    setError(null);
    setBusy(true);
    try {
      await onSubmit(email);
    } catch {
      setError('failed');
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} noValidate className="rounded-lg border border-border bg-muted/50 p-3 space-y-2">
      <p className="text-sm text-foreground">{labels.prompt}</p>
      <div className="flex gap-2">
        <Input
          type="email"
          autoComplete="email"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder={labels.placeholder}
          aria-label={labels.placeholder}
          aria-invalid={error === 'invalid'}
        />
        <Button type="submit" size="sm" disabled={busy}>
          {labels.submit}
        </Button>
      </div>
      {error && (
        <p className="text-xs text-destructive">{error === 'invalid' ? labels.invalid : labels.failed}</p>
      )}
    </form>
  );
}

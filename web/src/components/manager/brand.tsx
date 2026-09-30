"use client";

/**
 * manager 的品牌维度。manager 只在 kaitu 站点有一个入口，但管理两个品牌的数据：
 * - 全局筛选（ManagerBrandProvider + ManagerBrandSelect + useManagerBrand）：列表/统计页
 *   把 brandParam 作为 ?brand= 传给 /app/*；「全部」= 不传（API 不过滤）。
 * - BrandPicker：品牌自有实体的创建表单必须显式选品牌（无默认值——API 拒绝空 brand）。
 * - BrandBadge：带 brand 的行展示归属品牌。
 * 展示名只从品牌注册表（lib/brands.ts）取，本文件不写品牌展示字面量（tests/brand-guard.test.ts）。
 */
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { KAITU, OVERLEAP, brandById, type BrandId } from "@/lib/brands";
import { cn } from "@/lib/utils";

export type ManagerBrandFilter = "all" | BrandId;

export const MANAGER_BRAND_IDS: readonly BrandId[] = [KAITU.id, OVERLEAP.id];
export const MANAGER_BRAND_STORAGE_KEY = "manager.brandFilter";

export function isBrandId(v: unknown): v is BrandId {
  return typeof v === "string" && (MANAGER_BRAND_IDS as readonly string[]).includes(v);
}

export function brandLabel(id: BrandId): string {
  return brandById(id).wordmark;
}

function readStored(): ManagerBrandFilter {
  try {
    const v = window.localStorage.getItem(MANAGER_BRAND_STORAGE_KEY);
    return isBrandId(v) ? v : "all";
  } catch {
    return "all";
  }
}

function writeStored(v: ManagerBrandFilter) {
  try {
    window.localStorage.setItem(MANAGER_BRAND_STORAGE_KEY, v);
  } catch {
    // 隐私模式 / 存储被禁：选择只在本次会话内生效
  }
}

interface ManagerBrandContextValue {
  brand: ManagerBrandFilter;
  /** 传给 api 的 brand 参数；「全部」时为 undefined（不带 ?brand=）。 */
  brandParam: BrandId | undefined;
  setBrand: (b: ManagerBrandFilter) => void;
}

const ManagerBrandContext = createContext<ManagerBrandContextValue>({
  brand: "all",
  brandParam: undefined,
  setBrand: () => {},
});

export function ManagerBrandProvider({ children }: { children: React.ReactNode }) {
  const [brand, setBrandState] = useState<ManagerBrandFilter>("all");
  // 先读完持久化的选择再渲染子树：否则每个页面会先按「全部」拉一次、再按已存品牌拉一次。
  const [ready, setReady] = useState(false);

  useEffect(() => {
    setBrandState(readStored());
    setReady(true);
  }, []);

  const setBrand = useCallback((b: ManagerBrandFilter) => {
    setBrandState(b);
    writeStored(b);
  }, []);

  const value = useMemo<ManagerBrandContextValue>(
    () => ({ brand, brandParam: brand === "all" ? undefined : brand, setBrand }),
    [brand, setBrand],
  );

  return <ManagerBrandContext.Provider value={value}>{ready ? children : null}</ManagerBrandContext.Provider>;
}

export function useManagerBrand(): ManagerBrandContextValue {
  return useContext(ManagerBrandContext);
}

const selectClass =
  "h-9 rounded-md border border-input bg-background px-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring disabled:cursor-not-allowed disabled:opacity-60";

/** 全局品牌筛选（manager 顶栏）。 */
export function ManagerBrandSelect({ className }: { className?: string }) {
  const { brand, setBrand } = useManagerBrand();
  return (
    <select
      aria-label="品牌筛选"
      className={cn(selectClass, className)}
      value={brand}
      onChange={(e) => setBrand(isBrandId(e.target.value) ? e.target.value : "all")}
    >
      <option value="all">{"全部品牌"}</option>
      {MANAGER_BRAND_IDS.map((id) => (
        <option key={id} value={id}>
          {brandLabel(id)}
        </option>
      ))}
    </select>
  );
}

/** 创建表单的归属品牌选择：无预选值，value="" 表示尚未选择。 */
export function BrandPicker({
  value,
  onChange,
  disabled,
  className,
  id,
}: {
  value: BrandId | "";
  onChange: (b: BrandId) => void;
  disabled?: boolean;
  className?: string;
  id?: string;
}) {
  return (
    <select
      id={id}
      aria-label="归属品牌"
      className={cn(selectClass, "w-full", className)}
      value={value}
      disabled={disabled}
      onChange={(e) => {
        if (isBrandId(e.target.value)) onChange(e.target.value);
      }}
    >
      <option value="" disabled>
        {"请选择品牌"}
      </option>
      {MANAGER_BRAND_IDS.map((b) => (
        <option key={b} value={b}>
          {brandLabel(b)}
        </option>
      ))}
    </select>
  );
}

/** 行内品牌标记。 */
export function BrandBadge({ brand, className }: { brand: string | undefined | null; className?: string }) {
  if (!isBrandId(brand)) {
    return <span className={cn("text-muted-foreground", className)}>{"—"}</span>;
  }
  return (
    <span
      className={cn(
        "inline-flex items-center rounded border px-1.5 py-0.5 text-xs font-medium whitespace-nowrap",
        brand === KAITU.id ? "border-red-200 bg-red-50 text-red-700" : "border-sky-200 bg-sky-50 text-sky-700",
        className,
      )}
    >
      {brandLabel(brand)}
    </span>
  );
}

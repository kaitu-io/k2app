import { Button } from "@/components/ui/button";

// Today only the manager (Chinese-only, no next-intl provider — useLocale()
// would throw there) uses this, so the locale is an optional prop rather than
// read from context. Unknown / absent locale → the original Simplified labels.
const LABELS: Record<string, { prev: string; next: string }> = {
  "zh-CN": { prev: "上一页", next: "下一页" },
  "en-US": { prev: "Previous", next: "Next" },
  ko: { prev: "이전", next: "다음" },
  ja: { prev: "前へ", next: "次へ" },
  ru: { prev: "Назад", next: "Далее" },
};

interface PaginationProps {
  currentPage: number;
  totalPages: number;
  onPageChange: (page: number) => void;
  className?: string;
  /** Page locale (public `[locale]` pages); defaults to zh-CN. */
  locale?: string;
}

export function Pagination({
  currentPage,
  totalPages,
  onPageChange,
  className = "",
  locale,
}: PaginationProps) {
  const labels = (locale && LABELS[locale]) || LABELS["zh-CN"];
  // 生成页码数组，显示当前页前后各2页
  const getPageNumbers = () => {
    const pages: (number | string)[] = [];
    const maxVisible = 5; // 最多显示5个页码按钮

    if (totalPages <= maxVisible) {
      // 如果总页数小于等于最大可见数，显示所有页
      for (let i = 1; i <= totalPages; i++) {
        pages.push(i);
      }
    } else {
      // 总是显示第一页
      pages.push(1);

      // 计算当前页周围的页码
      const start = Math.max(2, currentPage - 1);
      const end = Math.min(totalPages - 1, currentPage + 1);

      // 如果 start > 2，添加省略号
      if (start > 2) {
        pages.push('...');
      }

      // 添加中间的页码
      for (let i = start; i <= end; i++) {
        pages.push(i);
      }

      // 如果 end < totalPages - 1，添加省略号
      if (end < totalPages - 1) {
        pages.push('...');
      }

      // 总是显示最后一页
      if (totalPages > 1) {
        pages.push(totalPages);
      }
    }

    return pages;
  };

  if (totalPages <= 1) {
    return null; // 只有一页时不显示分页
  }

  return (
    <div className={`flex items-center justify-center gap-2 ${className}`}>
      <Button
        variant="outline"
        size="sm"
        onClick={() => onPageChange(currentPage - 1)}
        disabled={currentPage === 1}
      >
        {labels.prev}
      </Button>

      {getPageNumbers().map((page, index) => {
        if (page === '...') {
          return (
            <span key={`ellipsis-${index}`} className="px-2 text-muted-foreground">
              {"..."}
            </span>
          );
        }

        const pageNum = page as number;
        return (
          <Button
            key={pageNum}
            variant={currentPage === pageNum ? "default" : "outline"}
            size="sm"
            onClick={() => onPageChange(pageNum)}
            className="min-w-[40px]"
          >
            {pageNum}
          </Button>
        );
      })}

      <Button
        variant="outline"
        size="sm"
        onClick={() => onPageChange(currentPage + 1)}
        disabled={currentPage === totalPages}
      >
        {labels.next}
      </Button>
    </div>
  );
}

interface PagitProps {
  currentPage: number;
  totalPages: number;
  onPageChange: (page: number) => void;
}

export function Pagit({ currentPage, totalPages, onPageChange }: PagitProps) {
  if (totalPages <= 1) return null;

  const pages: number[] = [];
  for (let i = 1; i <= totalPages; i++) {
    pages.push(i);
  }

  return (
    <div className="flex items-center justify-center gap-2 text-[--color-text-secondary] mt-4">
      <button
        onClick={() => onPageChange(currentPage - 1)}
        disabled={currentPage <= 1}
        className="px-2 py-1 rounded disabled:opacity-30"
        aria-label="Previous page"
      >
        &laquo;
      </button>
      {pages.map((page) => (
        <button
          key={page}
          onClick={() => onPageChange(page)}
          className={`px-3 py-1 rounded text-sm ${
            page === currentPage
              ? 'bg-[--color-primary] text-white'
              : 'hover:bg-[--color-glass-bg]'
          }`}
        >
          {page}
        </button>
      ))}
      <button
        onClick={() => onPageChange(currentPage + 1)}
        disabled={currentPage >= totalPages}
        className="px-2 py-1 rounded disabled:opacity-30"
        aria-label="Next page"
      >
        &raquo;
      </button>
    </div>
  );
}

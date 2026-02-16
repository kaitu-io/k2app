import { useEffect, useState, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { cloudApi } from '../api/cloud';

interface IssueItem {
  id: string;
  title: string;
  content: string;
  status: string;
  commentCount?: number;
  createdAt: string;
  updatedAt: string;
}

export function Issues() {
  const { t } = useTranslation('feedback');
  const navigate = useNavigate();
  const [issues, setIssues] = useState<IssueItem[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);

  const fetchIssues = useCallback(async (pageNum: number) => {
    setLoading(true);
    try {
      const resp = await cloudApi.getIssues(pageNum);
      const data = resp.data as { issues: IssueItem[]; hasMore: boolean } | undefined;
      if (data) {
        if (pageNum === 1) {
          setIssues(data.issues);
        } else {
          setIssues((prev) => [...prev, ...data.issues]);
        }
        setHasMore(data.hasMore);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchIssues(1);
  }, [fetchIssues]);

  const handleLoadMore = () => {
    const nextPage = page + 1;
    setPage(nextPage);
    fetchIssues(nextPage);
  };

  const formatRelativeTime = (dateStr: string): string => {
    const date = new Date(dateStr);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));
    if (diffDays === 0) return 'today';
    if (diffDays === 1) return '1d ago';
    if (diffDays < 30) return `${diffDays}d ago`;
    const diffMonths = Math.floor(diffDays / 30);
    return `${diffMonths}mo ago`;
  };

  return (
    <div className="p-4 space-y-4">
      <h1 className="text-xl font-semibold">{t('title')}</h1>

      {issues.length === 0 && !loading && (
        <p className="text-center text-[--color-text-secondary] py-8">{t('noIssues')}</p>
      )}

      <div className="space-y-3">
        {issues.map((issue) => (
          <button
            key={issue.id}
            onClick={() => navigate(`/issues/${issue.id}`)}
            className="w-full text-left rounded-lg p-4 bg-[--color-bg-paper] hover:translate-y-[-1px] transition-transform"
          >
            <div className="flex items-center justify-between mb-1">
              <h3 className="font-medium text-[--color-text-primary] truncate mr-2">
                {issue.title}
              </h3>
              <span
                className={`text-xs px-2 py-0.5 rounded-full whitespace-nowrap ${
                  issue.status === 'open'
                    ? 'bg-[--color-success]/20 text-[--color-success]'
                    : 'bg-[--color-text-disabled]/20 text-[--color-text-disabled]'
                }`}
              >
                {issue.status === 'open' ? t('open') : t('closed')}
              </span>
            </div>
            <div className="flex items-center gap-3 text-xs text-[--color-text-secondary]">
              <span>{formatRelativeTime(issue.createdAt)}</span>
              {issue.commentCount !== undefined && (
                <span>{issue.commentCount} {t('comments')}</span>
              )}
            </div>
          </button>
        ))}
      </div>

      {hasMore && (
        <div className="flex justify-center pt-2">
          <button
            onClick={handleLoadMore}
            disabled={loading}
            className="py-2 px-6 rounded-lg border border-[--color-primary] text-[--color-primary] hover:bg-[--color-primary]/10 transition-colors disabled:opacity-50"
          >
            {t('loadMore')}
          </button>
        </div>
      )}
    </div>
  );
}

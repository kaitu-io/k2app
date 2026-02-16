import { useEffect, useState, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useParams } from 'react-router-dom';
import { cloudApi } from '../api/cloud';

interface IssueData {
  id: string;
  title: string;
  content: string;
  status: string;
  createdAt: string;
  updatedAt: string;
}

interface CommentData {
  id: string;
  issueId: string;
  content: string;
  author: string;
  createdAt: string;
}

export function IssueDetail() {
  const { t } = useTranslation('feedback');
  const { id } = useParams<{ id: string }>();
  const [issue, setIssue] = useState<IssueData | null>(null);
  const [comments, setComments] = useState<CommentData[]>([]);
  const [commentText, setCommentText] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const fetchComments = useCallback(async () => {
    if (!id) return;
    const resp = await cloudApi.getIssueComments(id);
    const data = resp.data as CommentData[] | undefined;
    if (data) {
      setComments(data);
    }
  }, [id]);

  useEffect(() => {
    if (!id) return;

    const fetchIssue = async () => {
      const resp = await cloudApi.getIssueDetail(id);
      const data = resp.data as IssueData | undefined;
      if (data) {
        setIssue(data);
      }
    };

    fetchIssue();
    fetchComments();
  }, [id, fetchComments]);

  const handleSubmitComment = async () => {
    if (!id || !commentText.trim()) return;
    setSubmitting(true);
    try {
      await cloudApi.addComment(id, commentText);
      setCommentText('');
      await fetchComments();
    } finally {
      setSubmitting(false);
    }
  };

  const formatTime = (dateStr: string): string => {
    const date = new Date(dateStr);
    return date.toLocaleDateString();
  };

  if (!issue) {
    return null;
  }

  return (
    <div className="p-4 space-y-6">
      {/* Issue content card */}
      <div className="rounded-lg p-4 bg-[--color-bg-paper]">
        <h1 className="text-xl font-semibold text-[--color-text-primary] mb-2">
          {issue.title}
        </h1>
        <p className="text-sm text-[--color-text-secondary] mb-3">
          {formatTime(issue.createdAt)}
        </p>
        <p className="text-[--color-text-primary]">{issue.content}</p>
      </div>

      {/* Comments section */}
      <div>
        <h2 className="text-lg font-medium mb-3">{t('comments')}</h2>

        {comments.length === 0 ? (
          <p className="text-[--color-text-secondary] text-sm">{t('noComments')}</p>
        ) : (
          <div className="space-y-0">
            {comments.map((comment, index) => (
              <div key={comment.id}>
                {index > 0 && (
                  <div className="border-t border-[--color-text-disabled]/20 my-3" />
                )}
                <div className="flex gap-3">
                  <div className="w-8 h-8 rounded-full bg-[--color-primary]/20 flex items-center justify-center text-xs text-[--color-primary] shrink-0">
                    {comment.author.charAt(0).toUpperCase()}
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1">
                      <span className="text-sm font-medium text-[--color-text-primary]">
                        {comment.author}
                      </span>
                      <span className="text-xs text-[--color-text-secondary]">
                        {formatTime(comment.createdAt)}
                      </span>
                    </div>
                    <p className="text-sm text-[--color-text-primary]">{comment.content}</p>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Add comment form */}
      <div className="space-y-3">
        <textarea
          value={commentText}
          onChange={(e) => setCommentText(e.target.value)}
          placeholder={t('commentPlaceholder')}
          className="w-full rounded-lg p-3 bg-[--color-bg-paper] text-[--color-text-primary] border border-[--color-text-disabled]/30 resize-none min-h-[80px] placeholder:text-[--color-text-disabled]"
        />
        <button
          onClick={handleSubmitComment}
          disabled={submitting || !commentText.trim()}
          className="py-2 px-4 rounded-lg border border-[--color-primary] text-[--color-primary] hover:bg-[--color-primary]/10 transition-colors disabled:opacity-50"
        >
          {t('submit')}
        </button>
      </div>
    </div>
  );
}

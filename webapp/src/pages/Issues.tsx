import { useState, useEffect, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import {
  Box,
  Typography,
  Card,
  CardContent,
  Chip,
  Stack,
  Button,
  CircularProgress,
  Alert,
} from "@mui/material";
import {
  ChevronRight as ChevronRightIcon,
  ChatBubbleOutline as CommentIcon,
} from "@mui/icons-material";
import { useTranslation } from "react-i18next";

import BackButton from "../components/BackButton";
import { cloudApi } from "../services/cloud-api";
import type { GitHubIssue, GitHubIssuesListResponse } from "../services/api-types";

function formatRelativeTime(dateStr: string, t: (key: string) => string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / (1000 * 60));
  const diffHours = Math.floor(diffMs / (1000 * 60 * 60));
  const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));

  if (diffMins < 1) return t("common:common.justNow");
  if (diffMins < 60) return `${diffMins}m`;
  if (diffHours < 24) return `${diffHours}h`;
  if (diffDays < 30) return `${diffDays}d`;
  return date.toLocaleDateString();
}

export default function Issues() {
  const { t } = useTranslation();
  const navigate = useNavigate();

  const [issues, setIssues] = useState<GitHubIssue[]>([]);
  const [page, setPage] = useState(1);
  const [hasMore, setHasMore] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [isLoadingMore, setIsLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchIssues = useCallback(async (pageNum: number, append = false) => {
    if (append) {
      setIsLoadingMore(true);
    } else {
      setIsLoading(true);
    }
    setError(null);

    try {
      const response = await cloudApi.get<GitHubIssuesListResponse>(`/api/issues?page=${pageNum}&per_page=20`);

      if (response.code === 0 && response.data) {
        if (append) {
          setIssues((prev) => [...prev, ...response.data!.issues]);
        } else {
          setIssues(response.data.issues);
        }
        setHasMore(response.data.has_more);
        setPage(pageNum);
      } else {
        setError(t("ticket:issues.error"));
      }
    } catch (err) {
      console.error("[Issues] Failed to fetch issues:", err);
      setError(t("ticket:issues.error"));
    } finally {
      setIsLoading(false);
      setIsLoadingMore(false);
    }
  }, [t]);

  useEffect(() => {
    fetchIssues(1);
  }, [fetchIssues]);

  const handleLoadMore = () => {
    fetchIssues(page + 1, true);
  };

  return (
    <Box sx={{ width: "100%", height: "100%", position: "relative" }}>
      <BackButton to="/faq" />

      <Box
        sx={{
          width: "100%",
          height: "100%",
          display: "flex",
          justifyContent: "center",
          alignItems: "flex-start",
          pt: 9,
        }}
      >
        <Box
          sx={{
            width: 500,
            display: "flex",
            flexDirection: "column",
            gap: 1.5,
            overflow: "auto",
            height: "100%",
            pr: 0.5,
            pb: 4,
            "&::-webkit-scrollbar": { width: "8px" },
            "&::-webkit-scrollbar-track": { background: "transparent" },
            "&::-webkit-scrollbar-thumb": {
              background: (theme) =>
                theme.palette.mode === "dark"
                  ? "rgba(255,255,255,0.2)"
                  : "rgba(0,0,0,0.2)",
              borderRadius: "4px",
            },
          }}
        >
          {isLoading ? (
            <Box display="flex" justifyContent="center" py={4}>
              <CircularProgress />
            </Box>
          ) : error ? (
            <Alert severity="error" action={
              <Button color="inherit" size="small" onClick={() => fetchIssues(1)}>
                {t("common:common.retry")}
              </Button>
            }>
              {error}
            </Alert>
          ) : issues.length === 0 ? (
            <Card>
              <CardContent>
                <Stack alignItems="center" spacing={1} py={2}>
                  <Typography color="text.secondary">
                    {t("ticket:issues.empty")}
                  </Typography>
                  <Typography variant="body2" color="text.secondary">
                    {t("ticket:issues.emptyHint")}
                  </Typography>
                </Stack>
              </CardContent>
            </Card>
          ) : (
            <>
              {issues.map((issue) => (
                <Card
                  key={issue.number}
                  sx={{
                    cursor: "pointer",
                    transition: "all 0.2s",
                    "&:hover": { bgcolor: "action.hover" },
                  }}
                  onClick={() => navigate(`/issues/${issue.number}`)}
                >
                  <CardContent sx={{ py: 1.5, "&:last-child": { pb: 1.5 } }}>
                    <Stack direction="row" alignItems="center" justifyContent="space-between">
                      <Box flex={1} minWidth={0}>
                        <Stack direction="row" alignItems="center" gap={1} mb={0.5}>
                          <Chip
                            label={issue.state === "open" ? t("ticket:issues.stateOpen") : t("ticket:issues.stateClosed")}
                            size="small"
                            color={issue.state === "open" ? "warning" : "success"}
                            sx={{ height: 20, fontSize: "0.7rem" }}
                          />
                          {issue.has_official && (
                            <Chip
                              label={t("ticket:issues.officialBadge")}
                              size="small"
                              color="primary"
                              sx={{ height: 20, fontSize: "0.7rem" }}
                            />
                          )}
                        </Stack>
                        <Typography
                          variant="subtitle2"
                          sx={{
                            overflow: "hidden",
                            textOverflow: "ellipsis",
                            whiteSpace: "nowrap",
                          }}
                        >
                          {issue.title}
                        </Typography>
                        <Stack direction="row" alignItems="center" gap={1.5} mt={0.5}>
                          <Stack direction="row" alignItems="center" gap={0.5}>
                            <CommentIcon sx={{ fontSize: 14, color: "text.secondary" }} />
                            <Typography variant="caption" color="text.secondary">
                              {issue.comment_count}
                            </Typography>
                          </Stack>
                          <Typography variant="caption" color="text.secondary">
                            {formatRelativeTime(issue.created_at, t)}
                          </Typography>
                        </Stack>
                      </Box>
                      <ChevronRightIcon color="action" />
                    </Stack>
                  </CardContent>
                </Card>
              ))}

              {hasMore && (
                <Button
                  variant="outlined"
                  onClick={handleLoadMore}
                  disabled={isLoadingMore}
                  fullWidth
                  sx={{ mt: 1 }}
                >
                  {isLoadingMore ? (
                    <CircularProgress size={20} />
                  ) : (
                    t("ticket:issues.loadMore")
                  )}
                </Button>
              )}
            </>
          )}
        </Box>
      </Box>
    </Box>
  );
}

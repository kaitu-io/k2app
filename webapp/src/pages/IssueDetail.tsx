import { useState, useEffect, useCallback } from "react";
import { useParams } from "react-router-dom";
import {
  Box,
  Typography,
  Card,
  CardContent,
  Chip,
  Stack,
  CircularProgress,
  Alert,
  Button,
  Divider,
  TextField,
} from "@mui/material";
import { useTranslation } from "react-i18next";

import BackButton from "../components/BackButton";
import { cloudApi } from "../services/cloud-api";
import type { GitHubIssueDetail, GitHubComment } from "../services/api-types";

function formatRelativeTime(dateStr: string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / (1000 * 60));
  const diffHours = Math.floor(diffMs / (1000 * 60 * 60));
  const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));

  if (diffMins < 1) return "just now";
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffHours < 24) return `${diffHours}h ago`;
  if (diffDays < 30) return `${diffDays}d ago`;
  return date.toLocaleDateString();
}

export default function IssueDetail() {
  const { t } = useTranslation();
  const { number } = useParams<{ number: string }>();

  const [issue, setIssue] = useState<GitHubIssueDetail | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Comment form state
  const [commentBody, setCommentBody] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const fetchIssue = useCallback(async () => {
    if (!number) return;

    setIsLoading(true);
    setError(null);

    try {
      const response = await cloudApi.get<GitHubIssueDetail>(`/api/issues/${number}`);

      if (response.code === 0 && response.data) {
        setIssue(response.data);
      } else {
        setError(t("ticket:issues.error"));
      }
    } catch (err) {
      console.error("[IssueDetail] Failed to fetch issue:", err);
      setError(t("ticket:issues.error"));
    } finally {
      setIsLoading(false);
    }
  }, [number, t]);

  useEffect(() => {
    fetchIssue();
  }, [fetchIssue]);

  const handleSubmitComment = async () => {
    if (!commentBody.trim() || !number) return;

    setIsSubmitting(true);
    setSubmitError(null);

    try {
      const response = await cloudApi.post<GitHubComment>(`/api/issues/${number}/comments`, { body: commentBody.trim() });

      if (response.code === 0 && response.data) {
        // Add the new comment to the list
        setIssue((prev) =>
          prev ? { ...prev, comments: [...prev.comments, response.data!] } : prev
        );
        setCommentBody("");
      } else {
        setSubmitError(t("ticket:issues.commentFailed"));
      }
    } catch (err) {
      console.error("[IssueDetail] Failed to submit comment:", err);
      setSubmitError(t("ticket:issues.commentFailed"));
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Box sx={{ width: "100%", height: "100%", position: "relative" }}>
      <BackButton to="/issues" />

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
            <Alert
              severity="error"
              action={
                <Button color="inherit" size="small" onClick={fetchIssue}>
                  {t("common:common.retry")}
                </Button>
              }
            >
              {error}
            </Alert>
          ) : issue ? (
            <>
              {/* Issue Header */}
              <Card>
                <CardContent>
                  <Stack spacing={1.5}>
                    <Stack direction="row" gap={1}>
                      <Chip
                        label={
                          issue.state === "open"
                            ? t("ticket:issues.stateOpen")
                            : t("ticket:issues.stateClosed")
                        }
                        size="small"
                        color={issue.state === "open" ? "warning" : "success"}
                      />
                      {issue.has_official && (
                        <Chip
                          label={t("ticket:issues.officialBadge")}
                          size="small"
                          color="primary"
                        />
                      )}
                    </Stack>
                    <Typography variant="h6">{issue.title}</Typography>
                    <Typography
                      variant="body2"
                      color="text.secondary"
                      sx={{ whiteSpace: "pre-wrap" }}
                    >
                      {issue.body}
                    </Typography>
                    <Typography variant="caption" color="text.secondary">
                      {formatRelativeTime(issue.created_at)}
                    </Typography>
                  </Stack>
                </CardContent>
              </Card>

              {/* Comments Section */}
              <Card>
                <CardContent>
                  <Typography variant="subtitle2" gutterBottom>
                    {t("ticket:issues.commentsTitle")} ({issue.comments.length})
                  </Typography>

                  {issue.comments.length === 0 ? (
                    <Typography variant="body2" color="text.secondary" py={2}>
                      {t("ticket:issues.noComments")}
                    </Typography>
                  ) : (
                    <Stack spacing={1.5} mt={1}>
                      {issue.comments.map((comment, index) => (
                        <Box key={comment.id}>
                          {index > 0 && <Divider sx={{ mb: 1.5 }} />}
                          <Box
                            sx={{
                              pl: comment.is_official ? 1.5 : 0,
                              borderLeft: comment.is_official
                                ? "3px solid"
                                : "none",
                              borderColor: "primary.main",
                            }}
                          >
                            <Stack
                              direction="row"
                              alignItems="center"
                              gap={1}
                              mb={0.5}
                            >
                              {comment.is_official && (
                                <Chip
                                  label={t("ticket:issues.officialBadge")}
                                  size="small"
                                  color="primary"
                                  sx={{ height: 18, fontSize: "0.65rem" }}
                                />
                              )}
                              <Typography variant="caption" color="text.secondary">
                                {formatRelativeTime(comment.created_at)}
                              </Typography>
                            </Stack>
                            <Typography
                              variant="body2"
                              sx={{ whiteSpace: "pre-wrap" }}
                            >
                              {comment.body}
                            </Typography>
                          </Box>
                        </Box>
                      ))}
                    </Stack>
                  )}
                </CardContent>
              </Card>

              {/* Add Comment Form */}
              <Card>
                <CardContent>
                  <Stack spacing={1.5}>
                    <Typography variant="subtitle2">
                      {t("ticket:issues.addComment")}
                    </Typography>
                    <TextField
                      multiline
                      rows={3}
                      placeholder={t("ticket:issues.commentPlaceholder")}
                      value={commentBody}
                      onChange={(e) => setCommentBody(e.target.value)}
                      disabled={isSubmitting}
                      fullWidth
                      size="small"
                    />
                    {submitError && (
                      <Alert severity="error" onClose={() => setSubmitError(null)}>
                        {submitError}
                      </Alert>
                    )}
                    <Button
                      variant="contained"
                      onClick={handleSubmitComment}
                      disabled={isSubmitting || !commentBody.trim()}
                      fullWidth
                    >
                      {isSubmitting ? (
                        <CircularProgress size={20} color="inherit" />
                      ) : (
                        t("ticket:issues.submitComment")
                      )}
                    </Button>
                  </Stack>
                </CardContent>
              </Card>
            </>
          ) : null}
        </Box>
      </Box>
    </Box>
  );
}

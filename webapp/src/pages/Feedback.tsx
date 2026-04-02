import { useState, useEffect, useCallback } from "react";
import { useNavigate } from "react-router-dom";
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
  TextField,
  Badge,
} from "@mui/material";
import { Send as SendIcon } from "@mui/icons-material";
import { useTranslation } from "react-i18next";

import BackButton from "../components/BackButton";
import { cloudApi } from "../services/cloud-api";
import { useFeedbackStore } from "../stores/feedback.store";
import type {
  UserTicketListItem,
  UserTicketDetail,
  TicketReply,
} from "../services/api-types";

// ============ Helpers ============

function formatRelativeTime(ts: number, t: (key: string, opts?: Record<string, unknown>) => string): string {
  const now = Date.now();
  const diffMs = now - ts * 1000;
  const diffMins = Math.floor(diffMs / (1000 * 60));
  const diffHours = Math.floor(diffMs / (1000 * 60 * 60));
  const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));

  if (diffMins < 1) return t("ticket:feedback.time.justNow");
  if (diffMins < 60) return t("ticket:feedback.time.minutesAgo", { count: diffMins });
  if (diffHours < 24) return t("ticket:feedback.time.hoursAgo", { count: diffHours });
  if (diffDays < 30) return t("ticket:feedback.time.daysAgo", { count: diffDays });
  const d = new Date(ts * 1000);
  return `${d.getMonth() + 1}/${d.getDate()}`;
}

function formatShortTime(ts: number): string {
  const d = new Date(ts * 1000);
  const month = d.getMonth() + 1;
  const day = d.getDate();
  const hours = d.getHours().toString().padStart(2, "0");
  const mins = d.getMinutes().toString().padStart(2, "0");
  return `${month}/${day} ${hours}:${mins}`;
}

const statusColorMap: Record<string, "warning" | "success" | "default"> = {
  open: "warning",
  resolved: "success",
  closed: "default",
};

// ============ Ticket List ============

function TicketList({
  onSelect,
}: {
  onSelect: (id: number) => void;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [tickets, setTickets] = useState<UserTicketListItem[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchTickets = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const response = await cloudApi.get<{
        items: UserTicketListItem[];
        pagination: { total: number };
      }>("/api/user/tickets?pageSize=50");
      if (response.code === 0 && response.data) {
        setTickets(response.data.items || []);
      } else {
        setError(t("ticket:feedback.loadError"));
      }
    } catch (err) {
      console.error("[Feedback] Failed to fetch tickets:", err);
      setError(t("ticket:feedback.loadError"));
    } finally {
      setIsLoading(false);
    }
  }, [t]);

  useEffect(() => {
    fetchTickets();
  }, [fetchTickets]);

  return (
    <>
      <Stack direction="row" justifyContent="space-between" alignItems="center">
        <Typography variant="h6">{t("ticket:feedback.title")}</Typography>
        <Button
          variant="contained"
          size="small"
          onClick={() => navigate("/submit-ticket-form")}
        >
          {t("ticket:feedback.newTicket")}
        </Button>
      </Stack>

      {isLoading ? (
        <Box display="flex" justifyContent="center" py={4}>
          <CircularProgress />
        </Box>
      ) : error ? (
        <Alert
          severity="error"
          action={
            <Button color="inherit" size="small" onClick={fetchTickets}>
              {t("common:common.retry")}
            </Button>
          }
        >
          {error}
        </Alert>
      ) : tickets.length === 0 ? (
        <Card>
          <CardContent>
            <Stack spacing={2} alignItems="center" py={2}>
              <Typography variant="body2" color="text.secondary">
                {t("ticket:feedback.noTickets")}
              </Typography>
              <Typography variant="caption" color="text.secondary">
                {t("ticket:feedback.noTicketsHint")}
              </Typography>
              <Button
                variant="outlined"
                size="small"
                onClick={() => navigate("/submit-ticket-form")}
              >
                {t("ticket:feedback.newTicket")}
              </Button>
            </Stack>
          </CardContent>
        </Card>
      ) : (
        <Stack spacing={1}>
          {tickets.map((ticket) => (
            <Card
              key={ticket.id}
              sx={{ cursor: "pointer", "&:hover": { bgcolor: "action.hover" } }}
              onClick={() => onSelect(ticket.id)}
            >
              <CardContent sx={{ py: 1.5, "&:last-child": { pb: 1.5 } }}>
                <Stack spacing={0.5}>
                  <Stack direction="row" alignItems="center" gap={1}>
                    <Chip
                      label={t(`ticket:feedback.status.${ticket.status}`)}
                      size="small"
                      color={statusColorMap[ticket.status] || "default"}
                      sx={{ height: 20, fontSize: "0.7rem" }}
                    />
                    {ticket.userUnread > 0 && (
                      <Badge
                        badgeContent={ticket.userUnread}
                        color="error"
                        sx={{ "& .MuiBadge-badge": { fontSize: "0.65rem", height: 16, minWidth: 16 } }}
                      >
                        <Box />
                      </Badge>
                    )}
                    <Box flex={1} />
                    <Typography variant="caption" color="text.secondary">
                      {ticket.lastReplyAt
                        ? formatRelativeTime(ticket.lastReplyAt, t)
                        : formatRelativeTime(ticket.createdAt, t)}
                    </Typography>
                  </Stack>
                  <Typography
                    variant="body2"
                    sx={{
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                      whiteSpace: "nowrap",
                    }}
                  >
                    {ticket.content}
                  </Typography>
                  {ticket.lastReplyBy === "admin" && (
                    <Typography variant="caption" color="text.secondary">
                      {t("ticket:feedback.adminReplied")} {ticket.lastReplyAt ? formatRelativeTime(ticket.lastReplyAt, t) : ""}
                    </Typography>
                  )}
                </Stack>
              </CardContent>
            </Card>
          ))}
        </Stack>
      )}
    </>
  );
}

// ============ Ticket Detail ============

function TicketDetailView({
  ticketId,
}: {
  ticketId: number;
}) {
  const { t } = useTranslation();
  const fetchUnread = useFeedbackStore((s) => s.fetchUnread);

  const [ticket, setTicket] = useState<UserTicketDetail | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [replyContent, setReplyContent] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const fetchDetail = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const response = await cloudApi.get<UserTicketDetail>(
        `/api/user/tickets/${ticketId}`
      );
      if (response.code === 0 && response.data) {
        setTicket(response.data);
        // Resync unread count (backend cleared this ticket's unread)
        fetchUnread();
      } else {
        setError(t("ticket:feedback.loadError"));
      }
    } catch (err) {
      console.error("[Feedback] Failed to fetch ticket detail:", err);
      setError(t("ticket:feedback.loadError"));
    } finally {
      setIsLoading(false);
    }
  }, [ticketId, t, fetchUnread]);

  useEffect(() => {
    fetchDetail();
  }, [fetchDetail]);

  const handleReply = async () => {
    if (!replyContent.trim()) return;
    setIsSubmitting(true);
    setSubmitError(null);
    try {
      const response = await cloudApi.post<TicketReply>(
        `/api/user/tickets/${ticketId}/reply`,
        { content: replyContent.trim() }
      );
      if (response.code === 0 && response.data) {
        setTicket((prev) =>
          prev
            ? { ...prev, replies: [...prev.replies, response.data!] }
            : prev
        );
        setReplyContent("");
      } else {
        setSubmitError(t("ticket:feedback.replyFailed"));
      }
    } catch (err) {
      console.error("[Feedback] Failed to send reply:", err);
      setSubmitError(t("ticket:feedback.replyFailed"));
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <>
      {isLoading ? (
        <Box display="flex" justifyContent="center" py={4}>
          <CircularProgress />
        </Box>
      ) : error ? (
        <Alert
          severity="error"
          action={
            <Button color="inherit" size="small" onClick={fetchDetail}>
              {t("common:common.retry")}
            </Button>
          }
        >
          {error}
        </Alert>
      ) : ticket ? (
        <>
          {/* Status */}
          <Stack direction="row" alignItems="center" gap={1} mb={0.5}>
            <Chip
              label={t(`ticket:feedback.status.${ticket.status}`)}
              size="small"
              color={statusColorMap[ticket.status] || "default"}
            />
            <Typography variant="caption" color="text.secondary">
              {formatShortTime(ticket.createdAt)}
            </Typography>
          </Stack>

          {/* Original message (user, right-aligned) */}
          <Box display="flex" justifyContent="flex-end">
            <Box
              sx={{
                maxWidth: "85%",
                bgcolor: "primary.dark",
                borderRadius: 2,
                px: 2,
                py: 1,
              }}
            >
              <Typography variant="body2" sx={{ whiteSpace: "pre-wrap" }}>
                {ticket.content}
              </Typography>
            </Box>
          </Box>

          {/* Replies */}
          {ticket.replies.map((reply) => {
            const isUser = reply.senderType === "user";
            return (
              <Box
                key={reply.id}
                display="flex"
                justifyContent={isUser ? "flex-end" : "flex-start"}
              >
                <Box
                  sx={{
                    maxWidth: "85%",
                    bgcolor: isUser ? "primary.dark" : "background.paper",
                    borderRadius: 2,
                    borderLeft: isUser ? "none" : "3px solid",
                    borderColor: isUser ? undefined : "primary.main",
                    px: 2,
                    py: 1,
                  }}
                >
                  {!isUser && (
                    <Typography
                      variant="caption"
                      color="text.secondary"
                      display="block"
                      mb={0.5}
                    >
                      {reply.senderName}
                    </Typography>
                  )}
                  <Typography variant="body2" sx={{ whiteSpace: "pre-wrap" }}>
                    {reply.content}
                  </Typography>
                  <Typography
                    variant="caption"
                    color="text.secondary"
                    display="block"
                    mt={0.5}
                    textAlign="right"
                  >
                    {formatShortTime(reply.createdAt)}
                  </Typography>
                </Box>
              </Box>
            );
          })}

          {/* Reply input or closed notice */}
          {ticket.status === "closed" ? (
            <Alert severity="info">{t("ticket:feedback.closedNotice")}</Alert>
          ) : (
            <Card>
              <CardContent sx={{ py: 1.5, "&:last-child": { pb: 1.5 } }}>
                <Stack spacing={1}>
                  <TextField
                    multiline
                    rows={2}
                    placeholder={t("ticket:feedback.replyPlaceholder")}
                    value={replyContent}
                    onChange={(e) => setReplyContent(e.target.value)}
                    disabled={isSubmitting}
                    fullWidth
                    size="small"
                  />
                  {submitError && (
                    <Alert
                      severity="error"
                      onClose={() => setSubmitError(null)}
                    >
                      {submitError}
                    </Alert>
                  )}
                  <Box display="flex" justifyContent="flex-end">
                    <Button
                      variant="contained"
                      size="small"
                      onClick={handleReply}
                      disabled={isSubmitting || !replyContent.trim()}
                      endIcon={
                        isSubmitting ? (
                          <CircularProgress size={16} color="inherit" />
                        ) : (
                          <SendIcon />
                        )
                      }
                    >
                      {t("ticket:issues.submitComment")}
                    </Button>
                  </Box>
                </Stack>
              </CardContent>
            </Card>
          )}
        </>
      ) : null}
    </>
  );
}

// ============ Main Page ============

export default function Feedback() {
  const [selectedTicketId, setSelectedTicketId] = useState<number | null>(null);

  return (
    <Box sx={{ width: "100%", height: "100%", position: "relative" }}>
      {selectedTicketId !== null && (
        <BackButton onClick={() => setSelectedTicketId(null)} />
      )}

      <Box
        sx={{
          width: "100%",
          height: "100%",
          display: "flex",
          justifyContent: "center",
          alignItems: "flex-start",
          pt: selectedTicketId !== null ? 9 : 4,
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
          }}
        >
          {selectedTicketId !== null ? (
            <TicketDetailView ticketId={selectedTicketId} />
          ) : (
            <TicketList onSelect={setSelectedTicketId} />
          )}
        </Box>
      </Box>
    </Box>
  );
}

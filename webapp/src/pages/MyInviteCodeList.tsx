import { useEffect, useState, useRef } from "react";
import {
  Box,
  Typography,
  Paper,
  Stack,
  IconButton,
  Card,
  CardContent,
  Button,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Tooltip,
} from "@mui/material";
import {
  ShoppingCart as ShoppingCartIcon,
  Link as LinkIcon,
  Edit as EditIcon,
  Download as DownloadIcon,
  Lock as LockIcon,
} from "@mui/icons-material";
import { useTranslation } from "react-i18next";

import { MyInviteCode } from "../services/api-types";
import { formatTime } from "../utils/time";
import { LoadingState, EmptyInviteCodes } from "../components/LoadingAndEmpty";
import { useAlert } from "../stores";
import BackButton from "../components/BackButton";
import { useInviteCodeActions } from "../hooks/useInviteCodeActions";
import { cloudApi } from '../services/cloud-api';
import { delayedFocus } from '../utils/ui';

export default function MyInviteCodeList() {
  const { t } = useTranslation();
  const [inviteCodes, setInviteCodes] = useState<MyInviteCode[]>([]);
  const [loading, setLoading] = useState(false);
  const { showAlert } = useAlert();

  // 邀请码操作 hook
  const {
    shareInviteCode,
    copyPromotionLink,
    updateRemark,
    shareLinkLoading,
  } = useInviteCodeActions();

  // 编辑备注状态
  const [editingCode, setEditingCode] = useState<MyInviteCode | null>(null);

  // Ref for delayed focus in edit dialog
  const remarkInputRef = useRef<HTMLInputElement>(null);

  // Delayed focus when edit dialog opens
  useEffect(() => {
    if (!editingCode) return;
    const cancel = delayedFocus(() => remarkInputRef.current, 150);
    return cancel;
  }, [editingCode]);
  const [editRemark, setEditRemark] = useState("");

  // 移动端应用，统一使用卡片布局

  useEffect(() => {
    const fetchInviteCodes = async () => {
      setLoading(true);
      try {
        console.info(t('invite:invite.loading'));
        const response = await cloudApi.get<{ items: MyInviteCode[] }>('/api/invite/my-codes?page=0&pageSize=100');

        if (response.code === 0 && response.data) {
          console.info(`${t('invite:inviteCodeList.successGetInviteCodes')}: ${response.data.items.length}`);
          // 调试：打印每个邀请码的统计数据
          response.data.items.forEach((code: MyInviteCode, index: number) => {
            console.log(`[${index}] Code: ${code.code}, Register: ${code.registerCount}, Purchase: ${code.purchaseCount}, Reward: ${code.purchaseReward}`);
          });
          setInviteCodes(response.data.items);
        } else {
          console.warn('[MyInviteCodeList] Get invite codes failed:', response.code, response.message);
          showAlert(t('invite:invite.getInviteCodesFailed'), "error");
        }
      } catch (err) {
        console.error(`${t('invite:inviteCodeList.getInviteCodesException')}: ${err}`);
        showAlert(t('invite:invite.retry'), "error");
      } finally {
        setLoading(false);
      }
    };

    fetchInviteCodes();
  }, [showAlert, t]);

  // 打开编辑备注对话框
  const handleEditClick = (inviteCode: MyInviteCode) => {
    setEditingCode(inviteCode);
    setEditRemark(inviteCode.remark || "");
  };

  // 关闭编辑备注对话框
  const handleCloseEdit = () => {
    setEditingCode(null);
    setEditRemark("");
  };

  // 保存备注
  const handleSaveRemark = async () => {
    if (!editingCode) return;

    const success = await updateRemark(editingCode.code, editRemark);
    if (success) {
      // 更新本地列表数据
      setInviteCodes(inviteCodes.map(code =>
        code.code === editingCode.code
          ? { ...code, remark: editRemark }
          : code
      ));
      handleCloseEdit();
    }
  };

  // 分享按钮：直接系统分享/复制完整邀请内容（默认 7 天）
  const handleShareClick = (inviteCode: MyInviteCode) => {
    shareInviteCode(inviteCode);
  };

  // 复制链接按钮：直接复制推广链 baseURL/s/{code}
  const handleCopyLinkClick = (inviteCode: MyInviteCode) => {
    copyPromotionLink(inviteCode.code);
  };

  return (
    <Box
      sx={{
        width: "100%",
        py: 0.5,
        backgroundColor: "transparent",
        position: "relative"
      }}
    >
      <BackButton to="/invite" />
      {/* 简洁的顶部导航 */}
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 1.5, px: 1, pt: 7 }}>
        <Typography variant="h6" sx={{ flex: 1, fontWeight: 600 }} component="span">
          {t('invite:inviteCodeList.title')}
        </Typography>
      </Box>

      {/* 主内容区域 - 可滚动 */}
      <Box
        sx={{
          flex: 1,
          overflow: "auto",
          px: 1.5,
          py: 1,
        }}
      >
        <Box
          sx={{
            width: "100%",
          }}
        >
          {loading ? (
            <Paper
              sx={{
                borderRadius: 2,
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                minHeight: 400,
              }}
            >
              <LoadingState message={t('invite:invite.loading')} />
            </Paper>
          ) : inviteCodes.length === 0 ? (
            <Paper
              sx={{
                borderRadius: 2,
                p: 4,
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                minHeight: 400,
              }}
            >
              <EmptyInviteCodes />
            </Paper>
          ) : (
            // Card layout - 移动端布局
            <Stack spacing={1}>
              {inviteCodes.map((row) => (
                <Card
                  key={row.code}
                  elevation={0}
                  sx={{
                    borderRadius: 2,
                    border: "1px solid",
                    borderColor: "divider",
                    backgroundColor: "background.paper",
                    transition: "all 0.2s ease-in-out",
                    "&:active": {
                      backgroundColor: "action.hover",
                      transform: "scale(0.98)",
                    },
                  }}
                >
                  <CardContent sx={{ p: 1.5, "&:last-child": { pb: 1.5 } }}>
                    <Stack spacing={0.5}>
                      {/* Code and Status in one line */}
                      <Stack direction="row" alignItems="center" justifyContent="space-between" spacing={1}>
                        <Typography
                          variant="body2"
                          sx={{
                            fontFamily: "monospace",
                            fontWeight: 700,
                            color: "primary.main",
                            fontSize: "0.9rem",
                          }}
                        >
                          {row.code.toUpperCase()}
                        </Typography>
                        <Stack direction="row" alignItems="center" spacing={0.5}>
                          <ShoppingCartIcon sx={{ fontSize: 14, color: "success.main" }} />
                          <Typography
                            variant="caption"
                            fontWeight={600}
                            fontSize="0.75rem"
                            sx={{
                              color: row.purchaseCount > 0 ? "success.main" : "text.secondary",
                            }}
                          >
                            {row.purchaseCount}{t('invite:invite.people')}
                          </Typography>
                          <Typography variant="caption" fontSize="0.75rem" color="text.disabled">
                            /
                          </Typography>
                          <DownloadIcon sx={{ fontSize: 14, color: "primary.main" }} />
                          <Typography
                            variant="caption"
                            fontWeight={600}
                            fontSize="0.75rem"
                            sx={{
                              color: row.registerCount > 0 ? "primary.main" : "text.secondary",
                            }}
                          >
                            {row.registerCount}{t('invite:invite.people')}
                          </Typography>
                        </Stack>
                      </Stack>

                      {/* Remark - if exists */}
                      {row.remark && (
                        <Typography variant="body2" fontSize="0.8rem" color="text.secondary" sx={{ fontStyle: 'italic' }}>
                          {row.remark}
                        </Typography>
                      )}

                      {/* Created Time and Action Buttons in one line */}
                      <Stack direction="row" alignItems="center" justifyContent="space-between" spacing={0.5}>
                        <Typography variant="caption" color="text.secondary" fontSize="0.7rem">
                          {formatTime(row.createdAt)}
                        </Typography>

                        {/* Action Buttons Group */}
                        <Stack direction="row" spacing={0.5}>
                          {/* Share Button - Private link, narrow */}
                          <Tooltip title={t('invite:inviteCodeList.shareSecureTooltip')}>
                            <span>
                              <Button
                                variant="contained"
                                size="small"
                                startIcon={<LockIcon sx={{ fontSize: 12 }} />}
                                onClick={() => handleShareClick(row)}
                                disabled={shareLinkLoading}
                                sx={{
                                  textTransform: "none",
                                  borderRadius: 1,
                                  py: 0.25,
                                  px: 0.75,
                                  fontSize: "0.7rem",
                                  minHeight: 24,
                                  minWidth: 'auto',
                                  '& .MuiButton-startIcon': { marginRight: '4px', marginLeft: 0 },
                                }}
                              >
                                {t('invite:inviteCodeList.share')}
                              </Button>
                            </span>
                          </Tooltip>

                          {/* Secondary Actions - Icon Buttons */}
                          <Tooltip title={t('invite:inviteCodeList.copyLink')}>
                            <IconButton
                              size="small"
                              onClick={() => handleCopyLinkClick(row)}
                              sx={{
                                border: "1px solid",
                                borderColor: "divider",
                                borderRadius: 1,
                                width: 24,
                                height: 24,
                              }}
                            >
                              <LinkIcon sx={{ fontSize: 12 }} />
                            </IconButton>
                          </Tooltip>

                          <Tooltip title={t('invite:inviteCodeList.editRemark')}>
                            <IconButton
                              size="small"
                              onClick={() => handleEditClick(row)}
                              sx={{
                                border: "1px solid",
                                borderColor: "divider",
                                borderRadius: 1,
                                width: 24,
                                height: 24,
                              }}
                            >
                              <EditIcon sx={{ fontSize: 12 }} />
                            </IconButton>
                          </Tooltip>
                        </Stack>
                      </Stack>
                    </Stack>
                  </CardContent>
                </Card>
              ))}
            </Stack>
          )}

        </Box>
      </Box>

      {/* 编辑备注对话框 */}
      <Dialog
        open={editingCode !== null}
        onClose={handleCloseEdit}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>{t('invite:invite.editRemark')}</DialogTitle>
        <DialogContent>
          <TextField
            inputRef={remarkInputRef}
            margin="dense"
            label={t('invite:invite.remark')}
            fullWidth
            variant="outlined"
            value={editRemark}
            onChange={(e) => setEditRemark(e.target.value)}
            onBlur={(e) => setEditRemark(e.target.value.trim())}
            placeholder={t('invite:invite.remarkPlaceholder')}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCloseEdit}>{t('common:common.cancel')}</Button>
          <Button onClick={handleSaveRemark} variant="contained">
            {t('common:common.save')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
import { useState, useEffect, useRef } from "react";
import {
  Box,
  Typography,
  List,
  ListItem,
  ListItemText,
  ListItemIcon,
  ListItemSecondaryAction,
  Button,
  IconButton,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Stack,
  Chip,
  Alert,
  Divider,
  Tooltip,
} from "@mui/material";
import {
  Add as AddIcon,
  Delete as DeleteIcon,
  Email as EmailIcon,
  Person as PersonIcon,
  AccessTime as AccessTimeIcon,
  Refresh as RefreshIcon,
} from "@mui/icons-material";
import { useTranslation } from "react-i18next";

import type { DataUser } from "../services/api-types";
import { ErrorInvalidArgument } from "../services/api-types";
import { formatTime } from "../utils/time";
import { useAlert } from "../stores";
import { LoadingCard } from "../components/LoadingAndEmpty";
import EmailTextField from "../components/EmailTextField";
import BackButton from "../components/BackButton";
import { cloudApi } from '../services/cloud-api';
import { delayedFocus } from '../utils/ui';

// 成员状态计算
type MemberStatus = {
  label: string;
  color: 'default' | 'error' | 'warning' | 'success';
};

function getMemberStatus(member: DataUser, t: (key: string, params?: any) => string): MemberStatus {
  if (!member.expiredAt) {
    return { label: t('account:memberManagement.notActivated'), color: 'default' };
  }

  // expiredAt 是 Unix 时间戳（秒），需要转换为毫秒
  const expiredAt = new Date(member.expiredAt * 1000);
  const now = new Date();

  if (expiredAt <= now) {
    return { label: t('account:memberManagement.expired'), color: 'error' };
  }

  const daysLeft = Math.ceil((expiredAt.getTime() - now.getTime()) / (1000 * 60 * 60 * 24));
  if (daysLeft <= 7) {
    return { label: `${daysLeft}${t('invite:invite.days')}${t('account:memberManagement.expired')}`, color: 'warning' };
  }

  return { label: t('account:memberManagement.valid'), color: 'success' };
}

// 邮箱验证
function isValidEmail(email: string): boolean {
  const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
  return emailRegex.test(email.trim());
}

export default function MemberManagement() {
  const { t } = useTranslation();
  const { showAlert } = useAlert();

  // 状态管理
  const [members, setMembers] = useState<DataUser[]>([]);
  const [loading, setLoading] = useState(true); // 初始设为 true
  const [addDialogOpen, setAddDialogOpen] = useState(false);
  const [newMemberEmail, setNewMemberEmail] = useState("");
  const [addingMember, setAddingMember] = useState(false);
  const [hasError, setHasError] = useState(false);

  // Ref for delayed focus in dialog
  const emailInputRef = useRef<HTMLDivElement>(null);

  // Delayed focus when dialog opens - avoids timing issues on old WebViews
  useEffect(() => {
    if (!addDialogOpen) return;
    const cancel = delayedFocus(
      () => emailInputRef.current?.querySelector('input') as HTMLInputElement | null,
      150
    );
    return cancel;
  }, [addDialogOpen]);

  // 获取成员列表
  const fetchMembers = async (silent = false) => {
    if (!silent) {
      setLoading(true);
      setHasError(false);
    }

    try {
      const response = await cloudApi.get<{ items: DataUser[] }>('/api/user/members');

      // 检查 response 是否存在
      if (!response) {
        console.error('[MemberManagement] API response is undefined');
        throw new Error(t('common:errors.api.responseFailed'));
      }

      const { code, message, data } = response;
      if (code === 0 && data) {
        const items = data.items || [];
        setMembers(items);
        setHasError(false);
        return true;
      } else {
        const errorMsg = message || t('account:memberManagement.getMembersFailed');
        console.error('[MemberManagement] API returned error: ' + JSON.stringify({ code, message }));
        if (!silent) {
          showAlert(errorMsg, 'error');
          window._platform?.showToast?.(errorMsg, 'error');
        }
        setHasError(true);
        return false;
      }
    } catch (error) {
      console.error('[MemberManagement] Failed to fetch members:', error);
      setHasError(true);
      if (!silent) {
        const errorMsg = t('account:memberManagement.getMembersFailedRetry');
        showAlert(errorMsg, 'error');
        window._platform?.showToast?.(errorMsg, 'error');
      }
      // 确保在错误情况下也清空成员列表
      setMembers([]);
      return false;
    } finally {
      // 确保无论如何都会设置 loading 为 false
      setLoading(false);
    }
  };

  // 添加成员
  const handleAddMember = async () => {
    const email = newMemberEmail.trim();

    if (!email) {
      const msg = t('account:memberManagement.emailRequired');
      showAlert(msg, 'warning');
      window._platform?.showToast?.(msg, 'warning');
      return;
    }

    if (!isValidEmail(email)) {
      const msg = t('account:memberManagement.invalidEmail');
      showAlert(msg, 'warning');
      window._platform?.showToast?.(msg, 'warning');
      return;
    }

    setAddingMember(true);
    try {
      const response = await cloudApi.post<DataUser>('/api/user/members', { memberEmail: email });

      // 检查 response 是否存在
      if (!response) {
        console.error('[MemberManagement] addMember API response is undefined');
        throw new Error(t('common:errors.api.responseFailed'));
      }

      const { code, message, data } = response;
      if (code === 0) {
        // 添加成功后，直接更新本地状态，避免重新请求
        if (data) {
          setMembers(prev => [...prev, data]);
        } else {
          // 如果没有返回数据，静默刷新列表
          await fetchMembers(true);
        }
        const successMsg = t('account:memberManagement.addMemberSuccess');
        showAlert(successMsg, 'success');
        window._platform?.showToast?.(successMsg, 'success');
        setAddDialogOpen(false);
        setNewMemberEmail("");
      } else if (code === ErrorInvalidArgument) {
        // 422 错误：邮箱已被使用
        const errorMsg = message || t('account:memberManagement.emailAlreadyInUse');
        console.error('[MemberManagement] Invalid argument: ' + JSON.stringify({ code, message }));
        showAlert(errorMsg, 'error');
        window._platform?.showToast?.(errorMsg, 'error');
        // 不关闭对话框，让用户可以修改邮箱
      } else {
        const errorMsg = message || t('account:memberManagement.addMemberFailed');
        console.error('[MemberManagement] Add member failed: ' + JSON.stringify({ code, message }));
        showAlert(errorMsg, 'error');
        window._platform?.showToast?.(errorMsg, 'error');
      }
    } catch (error) {
      console.error('[MemberManagement] Exception adding member:', error);
      const errorMsg = t('account:memberManagement.addMemberFailedRetry');
      showAlert(errorMsg, 'error');
      window._platform?.showToast?.(errorMsg, 'error');
    } finally {
      setAddingMember(false);
    }
  };

  // 移除成员
  const handleRemoveMember = async (member: DataUser) => {
    try {
      const response = await cloudApi.request('DELETE', `/api/user/members/${member.uuid}`);

      // 检查 response 是否存在
      if (!response) {
        console.error('[MemberManagement] removeMember API response is undefined');
        throw new Error(t('common:errors.api.responseFailed'));
      }

      const { code, message } = response;
      if (code === 0) {
        // 删除成功后，直接更新本地状态，避免重新请求
        setMembers(prev => prev.filter(m => m.uuid !== member.uuid));
        const successMsg = t('account:memberManagement.removeMemberSuccess');
        showAlert(successMsg, 'success');
        window._platform?.showToast?.(successMsg, 'success');
      } else {
        const errorMsg = message || t('account:memberManagement.removeMemberFailed');
        console.error('[MemberManagement] Remove member failed: ' + JSON.stringify({ code, message }));
        showAlert(errorMsg, 'error');
        window._platform?.showToast?.(errorMsg, 'error');
      }
    } catch (error) {
      console.error('[MemberManagement] Exception removing member:', error);
      const errorMsg = t('account:memberManagement.removeMemberFailedRetry');
      showAlert(errorMsg, 'error');
      window._platform?.showToast?.(errorMsg, 'error');
    }
  };

  // 关闭添加对话框
  const handleCloseAddDialog = () => {
    if (!addingMember) {
      setAddDialogOpen(false);
      setNewMemberEmail("");
    }
  };

  // 刷新成员列表
  const handleRefreshMembers = async () => {
    await fetchMembers();
  };

  // 初始化
  useEffect(() => {
    // fetchMembers 已经处理了所有错误和状态，直接调用即可
    fetchMembers();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []); // 只在组件挂载时执行一次

  return (
    <Box sx={{
      width: "100%",
      py: 0.25,
      backgroundColor: "transparent",
      position: "relative"
    }}>
      <BackButton to="/account" />
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 0.75, px: 0.75, pt: 7 }}>
        <Typography variant="body1" sx={{ flex: 1, fontWeight: 600 }} component="span">
          {t('account:memberManagement.title')}
        </Typography>
        <IconButton
          onClick={handleRefreshMembers}
          disabled={loading}
          size="small"
          sx={{ minWidth: 32 }}
        >
          <RefreshIcon fontSize="small" />
        </IconButton>
        <IconButton
          color="primary"
          onClick={() => setAddDialogOpen(true)}
          disabled={loading}
          size="small"
          sx={{ minWidth: 32 }}
        >
          <AddIcon fontSize="small" />
        </IconButton>
      </Box>

      {/* 说明信息 */}
      <Alert
        severity="info"
        sx={{
          mb: 0.75,
          mx: 0.75,
          py: 0.5,
          fontSize: '0.8rem',
          '& .MuiAlert-icon': {
            fontSize: '1.1rem',
            py: 0
          }
        }}
      >
        {t('account:memberManagement.description')}
      </Alert>

      {/* 成员列表内容 */}
      {loading ? (
        <Box sx={{ px: 0.75 }}>
          <LoadingCard message={t('account:memberManagement.loading')} />
        </Box>
      ) : hasError ? (
        <Box sx={{
          px: 0.75,
          borderRadius: 2,
          backgroundColor: (theme) => theme.palette.background.paper
        }}>
          <Box sx={{
            textAlign: 'center',
            py: 2.5
          }}>
            <Alert severity="error" sx={{ mb: 2 }}>
              {t('account:memberManagement.getMembersFailedRetry')}
            </Alert>
            <Button
              variant="contained"
              startIcon={<RefreshIcon />}
              onClick={() => fetchMembers()}
              size="small"
            >
              {t('common:common.retry')}
            </Button>
          </Box>
        </Box>
      ) : members.length === 0 ? (
        <Box sx={{
          px: 0.75,
          borderRadius: 2,
          backgroundColor: (theme) => theme.palette.background.paper
        }}>
          <Box sx={{
            textAlign: 'center',
            py: 2.5
          }}>
            <PersonIcon sx={{
              fontSize: 48,
              color: 'text.secondary',
              mb: 1.5
            }} />
            <Typography
              variant="body1"
              color="text.secondary"
              gutterBottom
              sx={{ fontWeight: 600 }}
            >
              {t('account:memberManagement.noMembers')}
            </Typography>
            <Typography
              variant="body2"
              color="text.secondary"
              sx={{
                mb: 2,
                fontSize: '0.8rem'
              }}
            >
              {t('account:memberManagement.noMembersDesc')}
            </Typography>
            <Button
              variant="contained"
              startIcon={<AddIcon />}
              onClick={() => setAddDialogOpen(true)}
              size="small"
            >
              {t('account:memberManagement.addFirstMember')}
            </Button>
          </Box>
        </Box>
      ) : (
        <Box sx={{
          borderRadius: 2,
          mx: 0.75,
          backgroundColor: (theme) => theme.palette.background.paper
        }}>
            <List sx={{ py: 0.5 }}>
              {members.map((member, index) => {
                const status = getMemberStatus(member, t);
                const memberEmail = member.loginIdentifies?.find(
                  (li) => li.type === 'email'
                )?.value;

                return (
                  <Box key={member.uuid}>
                    <ListItem
                      sx={{
                        py: 1,
                        px: 1,
                        '&:hover': {
                          bgcolor: 'action.hover',
                          borderRadius: 1,
                        },
                        transition: 'background-color 0.2s ease',
                      }}
                    >
                      <ListItemIcon sx={{ minWidth: 36 }}>
                        <EmailIcon fontSize="small" />
                      </ListItemIcon>
                      <ListItemText
                        primary={
                          <Stack direction="column" spacing={0.5}>
                            <Typography
                              variant="body2"
                              sx={{
                                fontSize: '0.875rem',
                                wordBreak: 'break-word',
                                lineHeight: 1.3
                              }}
                            >
                              {memberEmail || t('account:memberManagement.noEmail')}
                            </Typography>
                            <Chip
                              label={status.label}
                              color={status.color}
                              size="small"
                              variant="outlined"
                              sx={{
                                height: 20,
                                fontSize: '0.7rem',
                                alignSelf: 'flex-start',
                                '& .MuiChip-label': {
                                  px: 0.75,
                                  py: 0
                                }
                              }}
                            />
                          </Stack>
                        }
                        secondary={
                          <Stack
                            direction="column"
                            alignItems="flex-start"
                            spacing={0.25}
                            sx={{ mt: 0.5 }}
                          >
                            <Box component="span" sx={{
                              display: 'flex',
                              alignItems: 'center',
                              gap: 0.25
                            }}>
                              <AccessTimeIcon
                                fontSize="small"
                                color="action"
                                sx={{
                                  fontSize: '0.9rem'
                                }}
                              />
                              <Typography
                                variant="body2"
                                color="text.secondary"
                                sx={{ fontSize: '0.75rem' }}
                              >
                                {member.expiredAt
                                  ? formatTime(member.expiredAt)
                                  : t('account:memberManagement.notActivated')}
                              </Typography>
                            </Box>
                            <Typography
                              variant="body2"
                              color="text.secondary"
                              sx={{ fontSize: '0.7rem' }}
                            >
                              UUID: {member.uuid.slice(0, 6)}...
                            </Typography>
                          </Stack>
                        }
                        sx={{
                          mr: 4
                        }}
                      />
                      <ListItemSecondaryAction>
                        <Tooltip title={t('account:memberManagement.removeMember')}>
                          <IconButton
                            edge="end"
                            color="error"
                            onClick={() => handleRemoveMember(member)}
                            size="small"
                            sx={{
                              right: 8
                            }}
                          >
                            <DeleteIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      </ListItemSecondaryAction>
                    </ListItem>
                    {index < members.length - 1 && <Divider />}
                  </Box>
                );
              })}
            </List>
        </Box>
      )}

      {/* 添加成员对话框 */}
      <Dialog
        open={addDialogOpen}
        onClose={handleCloseAddDialog}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>{t('account:memberManagement.addMemberDialog')}</DialogTitle>
        <DialogContent>
          <EmailTextField
            ref={emailInputRef}
            margin="dense"
            label={t('account:memberManagement.emailLabel')}
            fullWidth
            variant="outlined"
            value={newMemberEmail}
            onChange={setNewMemberEmail}
            placeholder={t('account:memberManagement.emailPlaceholder')}
            disabled={addingMember}
            helperText={t('account:memberManagement.emailHelp')}
            sx={{ mt: 2 }}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !addingMember) {
                handleAddMember();
              }
            }}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCloseAddDialog} disabled={addingMember}>
            {t('common:common.cancel')}
          </Button>
          <Button
            onClick={handleAddMember}
            variant="contained"
            disabled={addingMember || !newMemberEmail.trim()}
          >
            {addingMember ? t('common:common.adding') : t('common:common.add')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
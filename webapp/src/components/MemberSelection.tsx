import { useState, useEffect } from "react";
import {
  Box,
  Card,
  Typography,
  Button,
  Checkbox,
  FormControlLabel,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Stack,
  Avatar,
  Divider,
} from "@mui/material";
import {
  PersonAdd as PersonAddIcon,
  Person as PersonIcon,
  Email as EmailIcon,
  AccessTime as AccessTimeIcon,
} from "@mui/icons-material";
import { useTranslation } from "react-i18next";
import { useAlert, useAuthStore } from "../stores";

import type { DataUser, AddMemberRequest, ListResult } from "../services/api-types";
import { ErrorInvalidArgument } from "../services/api-types";
import EmailTextField from "./EmailTextField";
import { cloudApi } from '../services/cloud-api';
import { cacheStore } from '../services/cache-store';

const MEMBERS_CACHE_KEY = 'api:user_members';

export interface MemberSelectionProps {
  selectedForMyself: boolean;
  selectedMemberUUIDs: string[];
  onSelectionChange: (forMyself: boolean, memberUUIDs: string[]) => void;
}

export default function MemberSelection({
  selectedForMyself,
  selectedMemberUUIDs,
  onSelectionChange,
}: MemberSelectionProps) {
  const { t } = useTranslation();
  const { showAlert } = useAlert();
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);

  const [members, setMembers] = useState<DataUser[]>([]);
  const [loading, setLoading] = useState(false);
  const [addDialogOpen, setAddDialogOpen] = useState(false);
  const [newMemberEmail, setNewMemberEmail] = useState("");
  const [addingMember, setAddingMember] = useState(false);

  // 获取成员列表
  const fetchMembers = async () => {
    setLoading(true);
    try {
      // SWR: return cache immediately, refresh in background
      const cached = cacheStore.get<ListResult<DataUser>>(MEMBERS_CACHE_KEY);
      if (cached) {
        const memberList = cached.items || [];
        setMembers(memberList);
        const allMemberUUIDs = memberList.map((member: { uuid: string }) => member.uuid);
        onSelectionChange(selectedForMyself, allMemberUUIDs);
        setLoading(false);
        // Background revalidate
        cloudApi.get<ListResult<DataUser>>('/api/user/members').then(res => {
          if (res.code === 0 && res.data) {
            cacheStore.set(MEMBERS_CACHE_KEY, res.data, { ttl: 180 });
            const freshList = res.data.items || [];
            setMembers(freshList);
          }
        });
        return;
      }

      const { code, message, data } = await cloudApi.get<ListResult<DataUser>>('/api/user/members');
      if (code === 0 && data) {
        cacheStore.set(MEMBERS_CACHE_KEY, data, { ttl: 180 });
        const memberList = data.items || [];
        setMembers(memberList);

        // Select all members by default
        const allMemberUUIDs = memberList.map((member: { uuid: string }) => member.uuid);
        onSelectionChange(selectedForMyself, allMemberUUIDs);
      } else {
        showAlert(message || t('purchase:memberSelection.getMembersFailed'), "error");
      }
    } catch (error) {
      console.error(`Failed to fetch members: ${error}`);
      showAlert(t('purchase:memberSelection.getMembersFailedRetry'), "error");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (isAuthenticated) {
      fetchMembers();
    }
  }, [isAuthenticated]);

  // 添加成员
  const handleAddMember = async () => {
    if (!newMemberEmail.trim()) {
      showAlert(t('purchase:memberSelection.emailRequired'), "error");
      return;
    }

    setAddingMember(true);
    try {
      const request: AddMemberRequest = {
        memberEmail: newMemberEmail.trim(),
      };

      const { code, message, data } = await cloudApi.post<DataUser>('/api/user/members', request);
      if (code === 0 && data) {
        // Invalidate cache since members list changed
        cacheStore.delete(MEMBERS_CACHE_KEY);

        setMembers(prev => [...prev, data]);

        // Select newly added member by default
        const updatedMemberUUIDs = [...selectedMemberUUIDs, data.uuid];
        onSelectionChange(selectedForMyself, updatedMemberUUIDs);

        setNewMemberEmail("");
        setAddDialogOpen(false);
        showAlert(t('purchase:memberSelection.addMemberSuccess'), "success");
      } else if (code === ErrorInvalidArgument) {
        // 422 error: email already in use
        showAlert(message || t('purchase:memberSelection.emailAlreadyInUse'), "error");
        // Keep dialog open so user can modify email
      } else {
        showAlert(message || t('purchase:memberSelection.addMemberFailed'), "error");
      }
    } catch (error) {
      console.error(`Failed to add member: ${error}`);
      showAlert(t('purchase:memberSelection.addMemberFailedRetry'), "error");
    } finally {
      setAddingMember(false);
    }
  };

  // 格式化过期时间
  const formatExpiredAt = (expiredAt: number) => {
    if (!expiredAt || expiredAt <= 0) {
      return t('purchase:memberSelection.notActivated');
    }
    
    const now = Date.now() / 1000;
    if (expiredAt < now) {
      return t('purchase:memberSelection.expired');
    }
    
    return new Date(expiredAt * 1000).toLocaleDateString();
  };

  // 处理选择变化
  const handleMyselfChange = (checked: boolean) => {
    onSelectionChange(checked, selectedMemberUUIDs);
  };

  const handleMemberChange = (memberUUID: string, checked: boolean) => {
    let newSelectedUUIDs = [...selectedMemberUUIDs];
    if (checked) {
      if (!newSelectedUUIDs.includes(memberUUID)) {
        newSelectedUUIDs.push(memberUUID);
      }
    } else {
      newSelectedUUIDs = newSelectedUUIDs.filter(uuid => uuid !== memberUUID);
    }
    onSelectionChange(selectedForMyself, newSelectedUUIDs);
  };

  // 获取主邮箱
  const getPrimaryEmail = (user: DataUser): string => {
    const emailIdentify = user.loginIdentifies.find(identify => identify.type === "email");
    return emailIdentify?.value || t('purchase:memberSelection.noEmail');
  };

  const hasAnySelection = selectedForMyself || selectedMemberUUIDs.length > 0;

  const totalSelectedCount = (selectedForMyself ? 1 : 0) + selectedMemberUUIDs.length;

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 2 }}>
        <Typography variant="subtitle1" sx={{ fontWeight: 600, fontSize: '1rem' }} component="span">
          {t('purchase:memberSelection.selectPaymentTarget')}
        </Typography>
        {totalSelectedCount > 0 && (
          <Box sx={{
            display: 'flex',
            alignItems: 'center',
            bgcolor: 'primary.50',
            border: '1px solid',
            borderColor: 'primary.main',
            borderRadius: '12px',
            px: 1.5,
            py: 0.3,
          }}>
            <Typography variant="body2" color="primary.main" sx={{ fontWeight: 600, fontSize: '0.8rem' }} component="span">
              {t('purchase:memberSelection.selectedCount', { count: totalSelectedCount })}
            </Typography>
          </Box>
        )}
      </Box>

      <Card variant="outlined" sx={{ borderRadius: 2, p: 2 }}>
        {loading ? (
          <Typography variant="body2" color="text.secondary" component="span">
            {t('purchase:memberSelection.loading')}
          </Typography>
        ) : (
          <Stack direction="column" spacing={2}>
            {/* 为自己充值选项 */}
            <FormControlLabel
              control={
                <Checkbox
                  checked={selectedForMyself}
                  onChange={(e) => handleMyselfChange(e.target.checked)}
                  color="primary"
                />
              }
              label={
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                  <Avatar sx={{ width: 32, height: 32, bgcolor: 'primary.main' }}>
                    <PersonIcon fontSize="small" />
                  </Avatar>
                  <Box>
                    <Typography variant="body1" fontWeight={600} component="span">
                      {t('purchase:memberSelection.myself')}
                    </Typography>
                    <Typography variant="body2" color="text.secondary" component="span">
                      {t('purchase:memberSelection.chargeForMyself')}
                    </Typography>
                  </Box>
                </Box>
              }
            />

            {members.length > 0 && <Divider />}

            {/* 成员列表 */}
            {members.map((member) => {
              const primaryEmail = getPrimaryEmail(member);
              const isSelected = selectedMemberUUIDs.includes(member.uuid);
              
              return (
                <FormControlLabel
                  key={member.uuid}
                  control={
                    <Checkbox
                      checked={isSelected}
                      onChange={(e) => handleMemberChange(member.uuid, e.target.checked)}
                      color="primary"
                    />
                  }
                  label={
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flex: 1 }}>
                      <Avatar sx={{ width: 32, height: 32, bgcolor: 'secondary.main' }}>
                        <PersonIcon fontSize="small" />
                      </Avatar>
                      <Box sx={{ flex: 1, minWidth: 0 }}>
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.5 }}>
                          <EmailIcon fontSize="small" color="action" />
                          <Typography variant="body1" fontWeight={600} noWrap component="span">
                            {primaryEmail}
                          </Typography>
                        </Box>
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                          <AccessTimeIcon fontSize="small" color="action" />
                          <Typography variant="body2" color="text.secondary" component="span">
                            {t('purchase:memberSelection.expiresAt')}: {formatExpiredAt(member.expiredAt)}
                          </Typography>
                        </Box>
                      </Box>
                    </Box>
                  }
                />
              );
            })}

            {/* 添加成员按钮 */}
            <Button
              startIcon={<PersonAddIcon />}
              variant="outlined"
              color="primary"
              onClick={() => setAddDialogOpen(true)}
              sx={{
                mt: 1,
                textTransform: "none",
                alignSelf: 'flex-start',
              }}
            >
              {members.length === 0 
                ? t('purchase:memberSelection.addFirstMember') 
                : t('purchase:memberSelection.addAnotherMember')
              }
            </Button>

            {/* 选择提示 */}
            {!hasAnySelection && (
              <Box sx={{
                mt: 2,
                p: 1.5,
                bgcolor: 'warning.50',
                borderRadius: 1,
                border: '1px solid',
                borderColor: 'warning.main',
              }}>
                <Typography variant="body2" color="warning.dark" component="span">
                  {t('purchase:memberSelection.selectAtLeastOne')}
                </Typography>
              </Box>
            )}
          </Stack>
        )}
      </Card>

      {/* 添加成员对话框 */}
      <Dialog open={addDialogOpen} onClose={() => setAddDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>{t('purchase:memberSelection.addMember')}</DialogTitle>
        <DialogContent>
          <EmailTextField
            fullWidth
            label={t('purchase:memberSelection.memberEmail')}
            value={newMemberEmail}
            onChange={setNewMemberEmail}
            placeholder={t('purchase:memberSelection.memberEmailPlaceholder')}
            sx={{ mt: 2 }}
          />
          <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }} component="span">
            {t('purchase:memberSelection.addMemberHint')}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setAddDialogOpen(false)}>
            {t('purchase:memberSelection.cancel')}
          </Button>
          <Button 
            variant="contained" 
            onClick={handleAddMember}
            disabled={addingMember || !newMemberEmail.trim()}
          >
            {addingMember ? t('purchase:memberSelection.adding') : t('purchase:memberSelection.add')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
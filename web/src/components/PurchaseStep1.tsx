"use client";

import { useState, useEffect } from "react";
import { useTranslations } from "next-intl";
import { useAuth } from "@/contexts/AuthContext";
import EmailLogin from "@/components/EmailLogin";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { toast } from "sonner";
import { api, ApiError, ErrorCode, type User, type AddMemberRequest } from "@/lib/api";
import { useAppConfig } from "@/contexts/AppConfigContext";
import { UserIcon, PlusIcon, Mail, Clock, LoaderIcon, GiftIcon } from "lucide-react";

// Cookie helper function
function getCookie(name: string): string | null {
  if (typeof document === 'undefined') return null;
  const nameEQ = name + "=";
  const ca = document.cookie.split(';');
  for (let i = 0; i < ca.length; i++) {
    let c = ca[i];
    while (c.charAt(0) === ' ') c = c.substring(1, c.length);
    if (c.indexOf(nameEQ) === 0) return c.substring(nameEQ.length, c.length);
  }
  return null;
}

export interface PurchaseStep1Props {
  selectedForMyself?: boolean;
  selectedMemberUUIDs?: string[];
  onMemberSelectionChange?: (forMyself: boolean, memberUUIDs: string[]) => void;
  onLoginSuccess?: () => void;
}

export default function PurchaseStep1({
  selectedForMyself: externalSelectedForMyself,
  selectedMemberUUIDs: externalSelectedMemberUUIDs,
  onMemberSelectionChange,
  onLoginSuccess
}: PurchaseStep1Props = {}) {
  // Get app config from context
  const { appConfig } = useAppConfig();
  const t = useTranslations();
  const { isAuthenticated: authContextIsAuthenticated } = useAuth();

  // Local authentication state - can be corrected independently
  const [isAuthenticated, setIsAuthenticated] = useState(authContextIsAuthenticated);

  // Check for invite code cookie
  const [inviteCodeFromCookie, setInviteCodeFromCookie] = useState<string | null>(null);

  useEffect(() => {
    const code = getCookie('kaitu_invite_code');
    if (code) {
      setInviteCodeFromCookie(code);
    }
  }, []);

  // Sync local state with context when context changes
  useEffect(() => {
    setIsAuthenticated(authContextIsAuthenticated);
  }, [authContextIsAuthenticated]);

  // Member selection state - use external props if provided, otherwise internal state
  const [internalSelectedForMyself, setInternalSelectedForMyself] = useState(false);
  const [internalSelectedMemberUUIDs, setInternalSelectedMemberUUIDs] = useState<string[]>([]);

  const selectedForMyself = externalSelectedForMyself ?? internalSelectedForMyself;
  const selectedMemberUUIDs = externalSelectedMemberUUIDs ?? internalSelectedMemberUUIDs;

  // Member management state
  const [members, setMembers] = useState<User[]>([]);
  const [loading, setLoading] = useState(false);
  const [addDialogOpen, setAddDialogOpen] = useState(false);
  const [newMemberEmail, setNewMemberEmail] = useState("");
  const [addingMember, setAddingMember] = useState(false);
  const [membersFetched, setMembersFetched] = useState(false);

  // Fetch members only when authenticated and not already fetched
  useEffect(() => {
    if (!isAuthenticated || membersFetched) {
      return;
    }

    let isMounted = true;
    const fetchMembers = async () => {
      setLoading(true);
      try {
        console.log('[PurchaseStep1] Fetching members...');
        const response = await api.getMembers({ autoRedirectToAuth: false });
        const memberList = response.items || [];
        console.log('[PurchaseStep1] Got members:', memberList.length);
        if (isMounted) {
          setMembers(memberList);
          setMembersFetched(true);
        }
      } catch (error) {
        console.error('[PurchaseStep1] Failed to fetch members:', error);
        if (isMounted) {
          // Check if it's an unauthorized error
          if (error instanceof ApiError && error.code === ErrorCode.NotLogin) {
            console.log('[PurchaseStep1] Detected 401 error, user is not authenticated');
            // Correct the local authentication state and reset selections
            setIsAuthenticated(false);
            setMembersFetched(false);
            setInternalSelectedForMyself(false);
            setInternalSelectedMemberUUIDs([]);
            // Don't show error toast for 401, the UI will show login form instead
          } else {
            // Show error for other failures
            toast.error(t('purchase.purchase.getMembersFailed'));
          }
        }
      } finally {
        if (isMounted) {
          setLoading(false);
        }
      }
    };

    fetchMembers();

    return () => {
      isMounted = false;
    };
  }, [isAuthenticated, membersFetched, t]);

  // Add member handler
  const handleAddMember = async () => {
    const email = newMemberEmail.trim();
    if (!email) {
      toast.error(t('purchase.purchase.emailRequired'));
      return;
    }

    setAddingMember(true);
    try {
      const request: AddMemberRequest = { memberEmail: email };
      const newMember = await api.addMember(request, { autoRedirectToAuth: false });

      setMembers(prev => [...prev, newMember]);
      setNewMemberEmail("");
      setAddDialogOpen(false);
      toast.success(t('purchase.purchase.addMemberSuccess'));
    } catch (error) {
      console.error('[PurchaseStep1] Failed to add member:', error);
      // Check if it's an unauthorized error
      if (error instanceof ApiError && error.code === ErrorCode.NotLogin) {
        console.log('[PurchaseStep1] Detected 401 error during add member, user is not authenticated');
        // Correct the local authentication state and reset selections
        setIsAuthenticated(false);
        setMembersFetched(false);
        setInternalSelectedForMyself(false);
        setInternalSelectedMemberUUIDs([]);
        // Close dialog
        setAddDialogOpen(false);
        // Don't show error toast for 401
      } else {
        toast.error(t('purchase.purchase.addMemberFailed'));
      }
    } finally {
      setAddingMember(false);
    }
  };

  // Format expired date
  const formatExpiredAt = (expiredAt: number) => {
    if (!expiredAt || expiredAt <= 0) {
      return t('purchase.purchase.notActivated');
    }

    const now = Date.now() / 1000;
    if (expiredAt < now) {
      return t('purchase.purchase.expired');
    }

    return new Date(expiredAt * 1000).toLocaleDateString();
  };

  // Handle selection changes - use external callback if provided, otherwise internal state
  const handleMyselfChange = (checked: boolean) => {
    if (onMemberSelectionChange) {
      onMemberSelectionChange(checked, selectedMemberUUIDs);
    } else {
      setInternalSelectedForMyself(checked);
    }
  };

  const handleMemberChange = (memberUUID: string, checked: boolean) => {
    const newSelectedUUIDs = checked
      ? [...selectedMemberUUIDs, memberUUID].filter((uuid, index, arr) => arr.indexOf(uuid) === index)
      : selectedMemberUUIDs.filter(uuid => uuid !== memberUUID);

    if (onMemberSelectionChange) {
      onMemberSelectionChange(selectedForMyself, newSelectedUUIDs);
    } else {
      setInternalSelectedMemberUUIDs(newSelectedUUIDs);
    }
  };

  // Get primary email
  const getPrimaryEmail = (user: User): string => {
    const emailIdentify = user.loginIdentifies.find(identify => identify.type === "email");
    return emailIdentify?.value || t('purchase.purchase.noEmail');
  };

  const hasAnySelection = selectedForMyself || selectedMemberUUIDs.length > 0;

  return (
    <Card>
      <CardHeader className="pb-0">
        <CardTitle className="flex items-center gap-3 sm:gap-2">
          <div className="flex items-center justify-center w-10 h-10 sm:w-8 sm:h-8 bg-primary text-primary-foreground rounded-full text-base sm:text-sm font-bold flex-shrink-0">
            {t('common.step1')}
          </div>
          <div className="flex items-center gap-2 sm:gap-2">
            <UserIcon className="w-6 h-6 sm:w-5 sm:h-5 text-primary flex-shrink-0" />
            <span className="text-lg sm:text-base font-bold sm:font-semibold leading-tight text-foreground">
              {isAuthenticated ? t('purchase.purchase.selectPayTarget') : t('purchase.purchase.bindEmailAndSelectTarget')}
            </span>
          </div>
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-6 pt-0 px-3 sm:px-6">
        {/* Email Binding - For unauthenticated users only */}
        {!isAuthenticated && (
          <>
            {/* Invite Reward Prompt - Show when user came from invite link */}
            {inviteCodeFromCookie && appConfig?.inviteReward?.purchaseRewardDays && (
              <div className="p-4 bg-gradient-to-r from-amber-50 to-orange-50 dark:from-amber-950/30 dark:to-orange-950/30 border border-amber-200 dark:border-amber-800 rounded-lg mb-4">
                <div className="flex items-start gap-3">
                  <GiftIcon className="w-6 h-6 text-amber-600 dark:text-amber-400 flex-shrink-0 mt-0.5" />
                  <div>
                    <p className="text-base font-bold text-amber-800 dark:text-amber-200">
                      {t('purchase.purchase.inviteRewardTitle')}
                    </p>
                    <p className="text-sm text-amber-700 dark:text-amber-300 mt-1">
                      {t('purchase.purchase.inviteRewardDesc', { days: appConfig.inviteReward.purchaseRewardDays })}
                    </p>
                  </div>
                </div>
              </div>
            )}
            <EmailLogin onLoginSuccess={onLoginSuccess} mode="bind" />
          </>
        )}

        {/* Member Selection - Only for authenticated users */}
        {isAuthenticated && (
          <div className="space-y-4 sm:space-y-6">

            {loading ? (
              <div className="flex items-center justify-center py-4">
                <LoaderIcon className="w-5 h-5 animate-spin mr-2" />
                <span className="text-sm text-muted-foreground">
                  {t('purchase.purchase.loading')}
                </span>
              </div>
            ) : (
              <>
                {/* For myself option */}
                <div className="flex items-center space-x-3 sm:space-x-4 p-3 sm:p-3 rounded-lg border touch-manipulation hover:border-primary/50 transition-colors">
                  <Checkbox
                    id="myself"
                    checked={selectedForMyself}
                    onCheckedChange={handleMyselfChange}
                    className="w-5 h-5 sm:w-4 sm:h-4"
                  />
                  <div className="flex items-center space-x-3 sm:space-x-3 flex-1">
                    <div className="w-10 h-10 sm:w-8 sm:h-8 rounded-full bg-primary flex items-center justify-center flex-shrink-0">
                      <UserIcon className="w-5 h-5 sm:w-4 sm:h-4 text-primary-foreground" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <Label htmlFor="myself" className="text-lg sm:text-base font-bold sm:font-medium cursor-pointer block leading-tight">
                        {t('purchase.purchase.myself')}
                      </Label>
                      <p className="text-base sm:text-sm text-muted-foreground mt-1 sm:mt-0 leading-relaxed">
                        {t('purchase.purchase.chargeForMyself')}
                      </p>
                    </div>
                  </div>
                </div>

                {/* Members list */}
                {members.map((member) => {
                  const primaryEmail = getPrimaryEmail(member);
                  const isSelected = selectedMemberUUIDs.includes(member.uuid);

                  return (
                    <div
                      key={member.uuid}
                      className="flex items-center space-x-3 sm:space-x-4 p-3 sm:p-3 rounded-lg border touch-manipulation hover:border-primary/50 transition-colors"
                    >
                      <Checkbox
                        id={`member-${member.uuid}`}
                        checked={isSelected}
                        onCheckedChange={(checked) => handleMemberChange(member.uuid, !!checked)}
                        className="w-5 h-5 sm:w-4 sm:h-4 mt-1 sm:mt-0"
                      />
                      <div className="flex items-center space-x-3 sm:space-x-3 flex-1 min-w-0">
                        <div className="w-10 h-10 sm:w-8 sm:h-8 rounded-full bg-secondary flex items-center justify-center flex-shrink-0">
                          <UserIcon className="w-5 h-5 sm:w-4 sm:h-4 text-secondary-foreground" />
                        </div>
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center space-x-2 mb-2 sm:mb-1">
                            <Mail className="w-5 h-5 sm:w-4 sm:h-4 text-muted-foreground flex-shrink-0" />
                            <Label
                              htmlFor={`member-${member.uuid}`}
                              className="text-lg sm:text-base font-bold sm:font-medium cursor-pointer truncate leading-tight"
                            >
                              {primaryEmail}
                            </Label>
                          </div>
                          <div className="flex items-center space-x-2">
                            <Clock className="w-5 h-5 sm:w-4 sm:h-4 text-muted-foreground flex-shrink-0" />
                            <span className="text-base sm:text-sm text-muted-foreground leading-relaxed">
                              {t('purchase.purchase.expiresAt')}{t('common.colon')}{formatExpiredAt(member.expiredAt)}
                            </span>
                          </div>
                        </div>
                      </div>
                    </div>
                  );
                })}

                {/* Add member button */}
                <Dialog open={addDialogOpen} onOpenChange={setAddDialogOpen}>
                  <DialogTrigger asChild>
                    <Button
                      variant="outline"
                      className="w-full justify-start"
                    >
                      <PlusIcon className="w-4 h-4 mr-2" />
                      {members.length === 0
                        ? t('purchase.purchase.addFirstMember')
                        : t('purchase.purchase.addAnotherMember')
                      }
                    </Button>
                  </DialogTrigger>
                  <DialogContent>
                    <DialogHeader>
                      <DialogTitle>{t('purchase.purchase.addMember')}</DialogTitle>
                      <DialogDescription>
                        {t('purchase.purchase.addMemberHint')}
                      </DialogDescription>
                    </DialogHeader>
                    <div className="space-y-4 py-4">
                      <div>
                        <Label htmlFor="email">{t('purchase.purchase.memberEmail')}</Label>
                        <Input
                          id="email"
                          type="email"
                          value={newMemberEmail}
                          onChange={(e) => setNewMemberEmail(e.target.value)}
                          placeholder={t('purchase.purchase.memberEmailPlaceholder')}
                        />
                      </div>
                    </div>
                    <DialogFooter>
                      <Button
                        variant="outline"
                        onClick={() => setAddDialogOpen(false)}
                      >
                        {t('purchase.purchase.cancel')}
                      </Button>
                      <Button
                        onClick={handleAddMember}
                        disabled={addingMember || !newMemberEmail.trim()}
                      >
                        {addingMember ? t('purchase.purchase.adding') : t('purchase.purchase.add')}
                      </Button>
                    </DialogFooter>
                  </DialogContent>
                </Dialog>

                {/* Selection hint */}
                {!hasAnySelection && (
                  <div className="p-3 sm:p-3 bg-destructive/10 border border-destructive/20 rounded-lg">
                    <p className="text-base sm:text-sm text-destructive font-medium leading-relaxed">
                      {t('purchase.purchase.selectAtLeastOne')}
                    </p>
                  </div>
                )}
              </>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
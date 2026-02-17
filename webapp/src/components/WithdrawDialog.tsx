import { useState, useEffect } from 'react';
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  TextField,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Alert,
  Stack,
  Chip,
  Typography,
  InputAdornment,
  CircularProgress,
} from '@mui/material';
import {
  CheckCircle as CheckCircleIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';

import { useAlert } from '../stores';
import type { WithdrawAccount, Withdraw, CreateWithdrawRequest } from '../services/api-types';
import { cloudApi } from '../services/cloud-api';

interface WithdrawDialogProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
  availableBalance: number; // 美分
  accounts: WithdrawAccount[];
}

export default function WithdrawDialog({
  open,
  onClose,
  onSuccess,
  availableBalance,
  accounts,
}: WithdrawDialogProps) {
  const { t } = useTranslation();
  const { showAlert } = useAlert();

  const [selectedAccountId, setSelectedAccountId] = useState<number | ''>('');
  const [amount, setAmount] = useState('');
  const [userRemark, setUserRemark] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const MIN_WITHDRAW_AMOUNT = 10; // 最低提现金额 $10

  // 自动选择默认账户
  useEffect(() => {
    if (open && accounts.length > 0 && selectedAccountId === '') {
      const defaultAccount = accounts.find(acc => acc.isDefault);
      if (defaultAccount) {
        setSelectedAccountId(defaultAccount.id);
      } else {
        setSelectedAccountId(accounts[0].id);
      }
    }
  }, [open, accounts, selectedAccountId]);

  // 重置表单
  useEffect(() => {
    if (!open) {
      setSelectedAccountId('');
      setAmount('');
      setUserRemark('');
      setSubmitting(false);
    }
  }, [open]);

  const handleMaxAmount = () => {
    setAmount((availableBalance / 100).toFixed(2));
  };

  const handleSubmit = async () => {
    // 验证
    if (!selectedAccountId) {
      showAlert(t('wallet:wallet.selectAccountFirst'), 'error');
      return;
    }

    const amountNum = parseFloat(amount);
    if (isNaN(amountNum) || amountNum <= 0) {
      showAlert(t('wallet:wallet.invalidAmount'), 'error');
      return;
    }

    if (amountNum < MIN_WITHDRAW_AMOUNT) {
      showAlert(t('wallet:wallet.amountHelper', { min: MIN_WITHDRAW_AMOUNT }), 'error');
      return;
    }

    const amountCents = Math.round(amountNum * 100);
    if (amountCents > availableBalance) {
      showAlert(t('wallet:wallet.insufficientBalance'), 'error');
      return;
    }

    setSubmitting(true);
    try {
      const requestData: CreateWithdrawRequest = {
        amount: amountCents,
        withdrawAccountId: selectedAccountId as number,
        userRemark: userRemark || undefined,
      };

      const response = await cloudApi.post<Withdraw>('/api/wallet/withdraws', requestData);

      if (response.code === 0) {
        showAlert(t('wallet:wallet.withdrawSuccess'), 'success');
        onSuccess();
        onClose();
      } else {
        console.error('[WithdrawDialog] Withdraw failed:', response.code, response.message);
        showAlert(t('wallet:wallet.withdrawFailed'), 'error');
      }
    } catch (error) {
      console.error('[WithdrawDialog] Failed to create withdraw request:', error);
      showAlert(t('wallet:wallet.withdrawFailedRetry'), 'error');
    } finally {
      setSubmitting(false);
    }
  };

  const truncateAddress = (address: string) => {
    if (address.length <= 16) return address;
    return `${address.slice(0, 8)}...${address.slice(-6)}`;
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>{t('wallet:wallet.withdrawTitle')}</DialogTitle>

      <DialogContent>
        {/* 可用余额提示 */}
        <Alert severity="info" sx={{ mb: 2 }}>
          <Typography variant="body2">
            {t('wallet:wallet.availableBalance')}:{' '}
            <strong>${(availableBalance / 100).toFixed(2)}</strong>
          </Typography>
        </Alert>

        {/* 选择提现账户 */}
        <FormControl fullWidth sx={{ mb: 2 }}>
          <InputLabel>{t('wallet:wallet.selectAccount')}</InputLabel>
          <Select
            value={selectedAccountId}
            onChange={(e) => setSelectedAccountId(e.target.value as number)}
            label={t('wallet:wallet.selectAccount')}
          >
            {accounts.map((account) => (
              <MenuItem key={account.id} value={account.id}>
                <Stack direction="row" spacing={1} alignItems="center" sx={{ width: '100%' }}>
                  <Chip label={account.accountType.toUpperCase()} size="small" color="primary" />
                  <Chip label={account.currency.toUpperCase()} size="small" variant="outlined" />
                  <Typography variant="body2" fontFamily="monospace" sx={{ flex: 1 }}>
                    {account.accountType === 'paypal' ? account.accountId : truncateAddress(account.accountId)}
                  </Typography>
                  {account.isDefault && (
                    <CheckCircleIcon fontSize="small" color="success" />
                  )}
                </Stack>
              </MenuItem>
            ))}
          </Select>
        </FormControl>

        {/* 输入提现金额 */}
        <TextField
          fullWidth
          label={t('wallet:wallet.withdrawAmount')}
          type="number"
          value={amount}
          onChange={(e) => setAmount(e.target.value)}
          InputProps={{
            startAdornment: <InputAdornment position="start">$</InputAdornment>,
            endAdornment: (
              <InputAdornment position="end">
                <Button size="small" onClick={handleMaxAmount}>
                  {t('wallet:wallet.max')}
                </Button>
              </InputAdornment>
            ),
          }}
          helperText={t('wallet:wallet.amountHelper', { min: MIN_WITHDRAW_AMOUNT })}
          sx={{ mb: 2 }}
        />

        {/* 用户备注 */}
        <TextField
          fullWidth
          label={t('wallet:wallet.remark')}
          value={userRemark}
          onChange={(e) => setUserRemark(e.target.value)}
          multiline
          rows={2}
          placeholder={t('wallet:wallet.remarkPlaceholder')}
        />
      </DialogContent>

      <DialogActions>
        <Button onClick={onClose} disabled={submitting}>
          {t('common:common.cancel')}
        </Button>
        <Button
          variant="contained"
          onClick={handleSubmit}
          disabled={
            submitting ||
            !selectedAccountId ||
            !amount ||
            parseFloat(amount) <= 0
          }
        >
          {submitting ? <CircularProgress size={20} /> : t('wallet:wallet.confirmWithdraw')}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

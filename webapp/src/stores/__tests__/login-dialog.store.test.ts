import { describe, it, expect, beforeEach } from 'vitest';
import { useLoginDialogStore } from '../login-dialog.store';

describe('useLoginDialogStore', () => {
  beforeEach(() => {
    useLoginDialogStore.setState({
      isOpen: false,
      trigger: null,
      message: null,
    });
  });

  describe('open / close', () => {
    it('test_login_dialog_open_close — open() sets isOpen=true, close() sets isOpen=false', () => {
      expect(useLoginDialogStore.getState().isOpen).toBe(false);

      useLoginDialogStore.getState().open();
      expect(useLoginDialogStore.getState().isOpen).toBe(true);

      useLoginDialogStore.getState().close();
      expect(useLoginDialogStore.getState().isOpen).toBe(false);
    });
  });

  describe('trigger context', () => {
    it('test_login_dialog_trigger_context — open(trigger, message) stores trigger context', () => {
      useLoginDialogStore.getState().open('purchase', 'Please login to continue');

      const state = useLoginDialogStore.getState();
      expect(state.isOpen).toBe(true);
      expect(state.trigger).toBe('purchase');
      expect(state.message).toBe('Please login to continue');
    });

    it('close() clears trigger context', () => {
      useLoginDialogStore.getState().open('settings', 'Login required');
      useLoginDialogStore.getState().close();

      const state = useLoginDialogStore.getState();
      expect(state.isOpen).toBe(false);
      expect(state.trigger).toBeNull();
      expect(state.message).toBeNull();
    });
  });
});

import { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuthStore } from '../stores/auth.store';
import { useVPNMachineStore } from '../stores/vpn-machine.store';
import {
  getPushPermission,
  onPushRoute,
  pushSupported,
  shouldOfferPushPrompt,
  syncPushRegistration,
} from '../services/push-service';
import PushPermissionPrompt from './PushPermissionPrompt';

/**
 * App-wide push wiring (spec §5.2 / §5.3). Renders nothing unless the native
 * plugin exists (iOS shells that shipped it):
 * - tap on a push → navigate to its whitelisted route (cold start included);
 * - each login session / resume → report the APNs token (never prompts);
 * - the first time the VPN connects → offer the in-app permission explainer.
 */
export default function PushCoordinator() {
  const supported = pushSupported();
  const navigate = useNavigate();
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const sessionEpoch = useAuthStore((s) => s.sessionEpoch);
  const vpnState = useVPNMachineStore((s) => s.state);
  const [promptOpen, setPromptOpen] = useState(false);
  const prevVpnState = useRef(vpnState);

  useEffect(() => {
    if (!supported) return undefined;
    return onPushRoute((route) => navigate(route));
  }, [supported, navigate]);

  // New login session (incl. account switch): rebind this device's token to the account.
  useEffect(() => {
    if (supported && isAuthenticated) void syncPushRegistration({ force: true });
  }, [supported, isAuthenticated, sessionEpoch]);

  // Back from iOS Settings with notifications just turned on → register then.
  useEffect(() => {
    if (!supported) return undefined;
    const onVisible = () => {
      if (document.visibilityState === 'visible' && useAuthStore.getState().isAuthenticated) {
        void syncPushRegistration();
      }
    };
    document.addEventListener('visibilitychange', onVisible);
    return () => document.removeEventListener('visibilitychange', onVisible);
  }, [supported]);

  useEffect(() => {
    const was = prevVpnState.current;
    prevVpnState.current = vpnState;
    if (!supported || !isAuthenticated) return;
    if (vpnState !== 'connected' || was === 'connected') return;
    let cancelled = false;
    void getPushPermission().then((permission) => {
      if (!cancelled && shouldOfferPushPrompt(permission)) setPromptOpen(true);
    });
    return () => {
      cancelled = true;
    };
  }, [supported, isAuthenticated, vpnState]);

  if (!supported) return null;
  return <PushPermissionPrompt open={promptOpen} onClose={() => setPromptOpen(false)} />;
}

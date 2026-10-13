import { useEffect, useState } from 'react';
import { fetchServiceBlocker, type ServiceBlocker } from '../services/service-blocker';

/** How often the blocker is re-read while the service is down. */
export const SERVICE_BLOCKER_POLL_MS = 2000;

/**
 * Polls the service blocker while `active` (the service is unreachable), so
 * the UI follows the user flipping the System Settings switch: blocker →
 * null means "switch is on, launchd is starting the daemon".
 *
 * Inactive → always null and no polling. Only the boolean input is an effect
 * dependency (no callback identities — see feedback_hook_callback_identity).
 */
export function useServiceBlocker(active: boolean): ServiceBlocker | null {
  const [blocker, setBlocker] = useState<ServiceBlocker | null>(null);

  useEffect(() => {
    if (!active) {
      setBlocker(null);
      return;
    }
    let cancelled = false;
    const read = async () => {
      const b = await fetchServiceBlocker();
      if (!cancelled) {
        // Keep the previous object when nothing changed so consumers don't re-render.
        setBlocker((prev) =>
          prev && b && prev.kind === b.kind && prev.itemName === b.itemName ? prev : b,
        );
      }
    };
    void read();
    const id = setInterval(() => void read(), SERVICE_BLOCKER_POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [active]);

  return blocker;
}

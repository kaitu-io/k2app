/**
 * Capacitor push bridge — wraps @capacitor/push-notifications (iOS / APNs only).
 *
 * Like capacitor-social-signin.ts, the plugin's JS package lives in mobile/ only;
 * the webapp reaches the native side through `registerPlugin('PushNotifications')`
 * with the minimal types below. Shells built before the plugin shipped have no
 * native `PushNotifications` → returns undefined and the whole push feature hides.
 *
 * Android is deliberately excluded: the plugin needs Firebase there, and
 * mobile/capacitor.config.ts keeps it out of the Android build.
 * Spec: docs/superpowers/specs/2026-11-03-ios-push-design.md §5.
 */
import { Capacitor, registerPlugin, type PluginListenerHandle } from '@capacitor/core';
import { K2Plugin } from 'k2-plugin';
import type { IPushBridge, PushPermission } from '../types/kaitu-core';

type NativePermission = 'prompt' | 'prompt-with-rationale' | 'granted' | 'denied';

interface PushNotificationsNative {
  checkPermissions(): Promise<{ receive: NativePermission }>;
  requestPermissions(): Promise<{ receive: NativePermission }>;
  register(): Promise<void>;
  addListener(event: 'registration', cb: (t: { value: string }) => void): Promise<PluginListenerHandle>;
  addListener(event: 'registrationError', cb: (e: { error: string }) => void): Promise<PluginListenerHandle>;
  addListener(
    event: 'pushNotificationActionPerformed',
    cb: (a: { actionId: string; notification: { data?: Record<string, unknown> } }) => void,
  ): Promise<PluginListenerHandle>;
}

const PushNotifications = registerPlugin<PushNotificationsNative>('PushNotifications');
/** @capacitor/app is installed in mobile/ (every shell has it); minimal typing, no webapp dependency. */
const AppInfo = registerPlugin<{ getInfo(): Promise<{ id: string }> }>('App');

/** APNs normally answers within a second; past this the registration is treated as failed. */
export const REGISTER_TIMEOUT_MS = 15_000;

/** iOS deep link to this app's page in Settings (UIApplication.openSettingsURLString). */
export const IOS_APP_SETTINGS_URL = 'app-settings:';

function mapPermission(p: NativePermission): PushPermission {
  if (p === 'granted') return 'granted';
  if (p === 'denied') return 'denied';
  return 'prompt';
}

export function buildCapacitorPush(os: string): IPushBridge | undefined {
  if (os !== 'ios') return undefined;
  if (!Capacitor.isPluginAvailable('PushNotifications')) return undefined;

  return {
    async checkPermission() {
      return mapPermission((await PushNotifications.checkPermissions()).receive);
    },

    async requestPermission() {
      return mapPermission((await PushNotifications.requestPermissions()).receive);
    },

    async register() {
      const handles: PluginListenerHandle[] = [];
      try {
        return await new Promise<string | null>((resolve) => {
          const timer = setTimeout(() => {
            console.warn('[push] APNs registration timed out');
            resolve(null);
          }, REGISTER_TIMEOUT_MS);
          const done = (v: string | null) => {
            clearTimeout(timer);
            resolve(v);
          };
          void (async () => {
            handles.push(await PushNotifications.addListener('registration', (t) => done(t.value || null)));
            handles.push(
              await PushNotifications.addListener('registrationError', (e) => {
                console.warn('[push] APNs registration failed:', e.error);
                done(null);
              }),
            );
            await PushNotifications.register();
          })().catch((err) => {
            console.warn('[push] register() threw:', err);
            done(null);
          });
        });
      } finally {
        for (const h of handles) void h.remove();
      }
    },

    async appId() {
      return (await AppInfo.getInfo()).id;
    },

    onTap(cb) {
      void PushNotifications.addListener('pushNotificationActionPerformed', (a) => {
        cb(a.notification?.data ?? {});
      });
    },

    async openSettings() {
      await K2Plugin.openUrl({ url: IOS_APP_SETTINGS_URL });
    },
  };
}

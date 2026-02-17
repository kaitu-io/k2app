/**
 * Update Notification Component
 *
 * Shows a notification banner when an app update is downloaded.
 * User can choose to "Update Now" (installs and restarts) or "Later" (installs on exit).
 */

import React from 'react';
import { useTranslation } from 'react-i18next';
import { useUpdater } from '../hooks/useUpdater';

const styles: Record<string, React.CSSProperties> = {
  container: {
    position: 'fixed',
    top: 0,
    left: 0,
    right: 0,
    zIndex: 9999,
    backgroundColor: '#1976d2',
    color: 'white',
    padding: '12px 16px',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    boxShadow: '0 2px 8px rgba(0,0,0,0.2)',
    fontFamily: 'system-ui, -apple-system, sans-serif',
    fontSize: '14px',
  },
  message: {
    flex: 1,
  },
  version: {
    fontWeight: 600,
  },
  buttons: {
    display: 'flex',
    gap: '8px',
  },
  buttonPrimary: {
    backgroundColor: 'white',
    color: '#1976d2',
    border: 'none',
    padding: '8px 16px',
    borderRadius: '4px',
    cursor: 'pointer',
    fontWeight: 600,
    fontSize: '13px',
  },
  buttonPrimaryDisabled: {
    backgroundColor: 'rgba(255,255,255,0.7)',
    color: '#1976d2',
    border: 'none',
    padding: '8px 16px',
    borderRadius: '4px',
    cursor: 'not-allowed',
    fontWeight: 600,
    fontSize: '13px',
    opacity: 0.8,
  },
  buttonSecondary: {
    backgroundColor: 'transparent',
    color: 'white',
    border: '1px solid rgba(255,255,255,0.5)',
    padding: '8px 16px',
    borderRadius: '4px',
    cursor: 'pointer',
    fontSize: '13px',
  },
  buttonSecondaryDisabled: {
    backgroundColor: 'transparent',
    color: 'rgba(255,255,255,0.5)',
    border: '1px solid rgba(255,255,255,0.3)',
    padding: '8px 16px',
    borderRadius: '4px',
    cursor: 'not-allowed',
    fontSize: '13px',
  },
};

export function UpdateNotification() {
  const { t } = useTranslation();
  const { isUpdateDownloaded, updateInfo, applyUpdateNow, dismissUpdate, isInstalling } = useUpdater();

  if (!isUpdateDownloaded || !updateInfo) {
    return null;
  }

  return (
    <div style={styles.container}>
      <div style={styles.message}>
        <span style={styles.version}>v{updateInfo.newVersion}</span>
        {' '}{isInstalling ? t('startup:app.installing', 'is being installed...') : t('startup:app.readyToInstall', 'is ready to install')}
      </div>
      <div style={styles.buttons}>
        <button
          style={isInstalling ? styles.buttonSecondaryDisabled : styles.buttonSecondary}
          onClick={dismissUpdate}
          disabled={isInstalling}
        >
          {t('startup:app.later', 'Later')}
        </button>
        <button
          style={isInstalling ? styles.buttonPrimaryDisabled : styles.buttonPrimary}
          onClick={applyUpdateNow}
          disabled={isInstalling}
        >
          {isInstalling ? t('startup:app.installing', 'Installing...') : t('startup:app.updateNow', 'Update Now')}
        </button>
      </div>
    </div>
  );
}

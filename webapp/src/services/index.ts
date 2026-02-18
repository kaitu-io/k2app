/**
 * Services exports
 *
 * 简化架构：直接使用 window._k2.run()
 */

// ==================== Types ====================

export * from './vpn-types';

// ==================== API Types ====================

export * from './api-types';

// ==================== Cache ====================

export * from './cache-store';

// ==================== Cloud API ====================

export { cloudApi } from './cloud-api';

// ==================== Types ====================

export type { SResponse } from '../types/kaitu-core';

// ==================== Web Platform ====================

export { webPlatform } from './web-platform';

// ==================== Antiblock ====================

export { resolveEntry, DEFAULT_ENTRY } from './antiblock';

// ==================== Auth Service ====================

export { authService, TOKEN_STORAGE_KEY, REFRESH_TOKEN_STORAGE_KEY } from './auth-service';
export type { TokenPair, TunnelCredentials } from './auth-service';

/**
 * Services exports
 *
 * 简化架构：直接使用 window._k2.run()
 */

// ==================== Types ====================

export * from './control-types';

// ==================== API Types ====================

export * from './api-types';

// ==================== Cache ====================

export * from './cache-store';

// ==================== API Wrapper ====================

export { k2api } from './k2api';
export type { K2ApiConfig, SResponse } from './k2api';

// ==================== Web Platform ====================

export { webPlatform } from './web-platform';

// ==================== Antiblock ====================

export { resolveEntry, DEFAULT_ENTRY } from './antiblock';

// ==================== Auth Service ====================

export { authService, TOKEN_STORAGE_KEY, REFRESH_TOKEN_STORAGE_KEY } from './auth-service';
export type { TokenPair, TunnelCredentials } from './auth-service';

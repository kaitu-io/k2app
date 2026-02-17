/**
 * Centralized Error Handler
 *
 * Professional error handling with:
 * - Automatic toast notifications
 * - User-friendly messages
 * - Type-safe error categorization
 * - Retry mechanisms
 * - Silent error options
 *
 * Usage:
 * ```typescript
 * // Automatic error handling with toast
 * await handleError(async () => {
 *   await api.deleteDevice(id);
 * }, {
 *   successMessage: t('devices.deleteSuccess'),
 *   errorMessage: t('devices.deleteFailed')
 * });
 *
 * // Custom error handling
 * try {
 *   const data = await api.getData();
 * } catch (error) {
 *   showErrorToast(error, t);
 * }
 * ```
 */

import { TFunction } from 'i18next';
import { useAlertStore } from '../stores/alert.store';
import { getErrorMessage, ERROR_CODES } from './errorCode';

// ============ Types ============

export interface SResponse<T = any> {
  code: number;
  message: string;
  data?: T;
}

export interface ErrorHandlerOptions {
  /** Success message to show (if operation succeeds) */
  successMessage?: string;
  /** Error message to show (overrides code-based message) */
  errorMessage?: string;
  /** Don't show any toast notifications */
  silent?: boolean;
  /** Callback when error occurs */
  onError?: (error: Error) => void;
  /** Callback when success */
  onSuccess?: () => void;
  /** Retry count (default: 0) */
  retryCount?: number;
  /** Retry delay in ms (default: 1000) */
  retryDelay?: number;
}

export interface ApiError extends Error {
  code?: number;
  response?: SResponse;
}

// ============ Core Error Handling ============

/**
 * Show error toast notification
 * Automatically maps error codes to user-friendly messages
 */
export function showErrorToast(error: unknown, t: TFunction): void {
  const { showAlert } = useAlertStore.getState();

  if (isApiError(error)) {
    const code = error.code || ERROR_CODES.INTERNAL_SERVER_ERROR;
    const message = getErrorMessage(code, t, error.message);
    showAlert(message, 'error');
  } else if (error instanceof Error) {
    showAlert(error.message, 'error');
  } else {
    showAlert(t('common:common.unknownError'), 'error');
  }
}

/**
 * Show success toast notification
 */
export function showSuccessToast(message: string): void {
  const { showAlert } = useAlertStore.getState();
  showAlert(message, 'success');
}

/**
 * Show warning toast notification
 */
export function showWarningToast(message: string): void {
  const { showAlert } = useAlertStore.getState();
  showAlert(message, 'warning');
}

/**
 * Show info toast notification
 */
export function showInfoToast(message: string): void {
  const { showAlert } = useAlertStore.getState();
  showAlert(message, 'info');
}

/**
 * Type guard for API errors
 */
export function isApiError(error: unknown): error is ApiError {
  return error instanceof Error && 'code' in error;
}

/**
 * Extract error message from various error types
 */
export function getErrorMessageText(error: unknown, t: TFunction, fallback?: string): string {
  if (isApiError(error) && error.code !== undefined) {
    return getErrorMessage(error.code, t, error.message);
  }
  if (error instanceof Error) {
    return error.message;
  }
  return fallback || t('common:common.unknownError');
}

/**
 * Create API error from response
 */
export function createApiError(response: SResponse, defaultMessage: string): ApiError {
  const error = new Error(response.message || defaultMessage) as ApiError;
  error.code = response.code;
  error.response = response;
  return error;
}

// ============ Async Function Wrapper ============

/**
 * Wrap async function with automatic error handling
 * Shows toast notifications and handles retries
 *
 * @example
 * ```typescript
 * await handleError(
 *   async () => await api.deleteDevice(id),
 *   {
 *     successMessage: t('devices.deleteSuccess'),
 *     errorMessage: t('devices.deleteFailed'),
 *     retryCount: 2
 *   }
 * );
 * ```
 */
export async function handleError<T>(
  fn: () => Promise<T>,
  options: ErrorHandlerOptions = {}
): Promise<T | null> {
  const {
    successMessage,
    errorMessage,
    silent = false,
    onError,
    onSuccess,
    retryCount = 0,
    retryDelay = 1000,
  } = options;

  let lastError: Error;
  let attempts = 0;

  while (attempts <= retryCount) {
    try {
      const result = await fn();

      if (!silent && successMessage) {
        showSuccessToast(successMessage);
      }

      onSuccess?.();
      return result;
    } catch (error) {
      lastError = error as Error;
      attempts++;

      // If more retries available, wait and continue
      if (attempts <= retryCount) {
        await sleep(retryDelay);
        continue;
      }

      // No more retries, handle error
      if (!silent) {
        if (errorMessage) {
          const { showAlert } = useAlertStore.getState();
          showAlert(errorMessage, 'error');
        } else if (error instanceof Error) {
          const { showAlert } = useAlertStore.getState();
          showAlert(error.message, 'error');
        }
      }

      onError?.(lastError);
      return null;
    }
  }

  return null;
}

/**
 * Validate API response and throw error if failed
 * Use this to convert SResponse to data or throw
 *
 * @example
 * ```typescript
 * const response = await k2api().exec('api_request', {...});
 * const data = validateResponse(response, t, 'Failed to load');
 * ```
 */
export function validateResponse<T>(
  response: SResponse<T>,
  t: TFunction,
  defaultError: string
): T {
  if (response.code !== ERROR_CODES.SUCCESS) {
    const message = getErrorMessage(response.code, t, response.message || defaultError);
    const error = new Error(message) as ApiError;
    error.code = response.code;
    error.response = response;
    throw error;
  }

  if (!response.data) {
    throw new Error(defaultError);
  }

  return response.data;
}

// ============ Retry Helper ============

async function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms));
}

/**
 * Retry a function with exponential backoff
 *
 * @example
 * ```typescript
 * const data = await retryWithBackoff(
 *   () => api.fetchData(),
 *   { maxRetries: 3, initialDelay: 500 }
 * );
 * ```
 */
export async function retryWithBackoff<T>(
  fn: () => Promise<T>,
  options: {
    maxRetries?: number;
    initialDelay?: number;
    maxDelay?: number;
    backoffFactor?: number;
  } = {}
): Promise<T> {
  const {
    maxRetries = 3,
    initialDelay = 1000,
    maxDelay = 30000,
    backoffFactor = 2,
  } = options;

  let lastError: Error;
  let delay = initialDelay;

  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    try {
      return await fn();
    } catch (error) {
      lastError = error as Error;

      if (attempt < maxRetries) {
        await sleep(Math.min(delay, maxDelay));
        delay *= backoffFactor;
      }
    }
  }

  throw lastError!;
}

// ============ Network Error Classification ============

/**
 * Classify network error for better user messages
 */
export function classifyNetworkError(error: Error): {
  isNetworkError: boolean;
  isTimeout: boolean;
  isConnectionRefused: boolean;
  isDNSError: boolean;
} {
  const message = error.message.toLowerCase();

  return {
    isNetworkError:
      message.includes('network') ||
      message.includes('fetch') ||
      message.includes('connection'),
    isTimeout:
      message.includes('timeout') ||
      message.includes('timed out'),
    isConnectionRefused:
      message.includes('refused') ||
      message.includes('econnrefused'),
    isDNSError:
      message.includes('dns') ||
      message.includes('getaddrinfo') ||
      message.includes('enotfound'),
  };
}

// ============ React Hook ============

/**
 * Hook for error handling in components
 * Provides easy access to error handling functions with i18n
 *
 * @example
 * ```typescript
 * function MyComponent() {
 *   const { handleError, showError, showSuccess } = useErrorHandler();
 *
 *   const handleDelete = async () => {
 *     await handleError(
 *       () => api.deleteItem(id),
 *       { successMessage: t('deleteSuccess') }
 *     );
 *   };
 * }
 * ```
 */
export function useErrorHandler(t: TFunction) {
  return {
    handleError: (fn: () => Promise<any>, options?: ErrorHandlerOptions) =>
      handleError(fn, options),
    showError: (error: unknown) => showErrorToast(error, t),
    showSuccess: showSuccessToast,
    showWarning: showWarningToast,
    showInfo: showInfoToast,
    validateResponse: (response: SResponse, defaultError: string) =>
      validateResponse(response, t, defaultError),
  };
}

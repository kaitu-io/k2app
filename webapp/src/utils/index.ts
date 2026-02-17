/**
 * Utils Index
 * Central export for utility functions
 */

// Error handling
export {
  handleError,
  showErrorToast,
  showSuccessToast,
  showWarningToast,
  showInfoToast,
  validateResponse,
  retryWithBackoff,
  isApiError,
  getErrorMessageText,
  createApiError,
  classifyNetworkError,
  useErrorHandler,
  type ErrorHandlerOptions,
  type ApiError,
  type SResponse,
} from './errorHandler';

// Error codes
export {
  getErrorMessage,
  handleResponseError,
  isSuccess,
  ERROR_CODES,
} from './errorCode';

// Time utilities
export { formatTime } from './time';

import type { Config } from './config.ts'

/**
 * Type alias for the fetch function signature, allowing injection of a custom
 * fetch implementation in tests.
 */
type FetchFn = (url: string | URL | Request, init?: RequestInit) => Promise<Response>

/**
 * HTTP client for the Kaitu Center API.
 *
 * Automatically injects the X-Access-Key authentication header on every request.
 * Constructs full request URLs from the configured base URL.
 *
 * @example
 * ```ts
 * const client = new CenterApiClient(config)
 * const data = await client.request('/api/v1/users')
 * ```
 */
export class CenterApiClient {
  private readonly config: Config
  private readonly fetchFn: FetchFn

  /**
   * Creates a new CenterApiClient.
   *
   * @param config - Configuration containing center.url and center.accessKey
   * @param fetchFn - Optional custom fetch function (defaults to global fetch). Used for testing.
   */
  constructor(config: Config, fetchFn: FetchFn = fetch) {
    this.config = config
    this.fetchFn = fetchFn
  }

  /**
   * Makes an authenticated HTTP request to the Center API.
   *
   * Constructs the full URL by joining the configured base URL with the given path.
   * Automatically adds the X-Access-Key header for authentication.
   * Merges any additional headers from `options.headers` with the auth header.
   *
   * @param path - The API path to request (e.g. '/api/v1/users'). Leading slash optional.
   * @param options - Optional fetch RequestInit options (method, body, headers, etc.)
   * @returns Parsed JSON response body
   * @throws {Error} On network failure or non-2xx HTTP response
   */
  async request(path: string, options?: RequestInit): Promise<unknown> {
    const baseUrl = this.config.center.url.replace(/\/$/, '')
    const normalizedPath = path.startsWith('/') ? path : `/${path}`
    const url = `${baseUrl}${normalizedPath}`

    const headers: Record<string, string> = {
      'X-Access-Key': this.config.center.accessKey,
      'Content-Type': 'application/json',
      ...(options?.headers as Record<string, string> | undefined),
    }

    const response = await this.fetchFn(url, {
      ...options,
      headers,
    })

    if (!response.ok) {
      throw new Error(
        `Center API request failed: ${options?.method ?? 'GET'} ${url} → HTTP ${response.status}`
      )
    }

    return response.json() as Promise<unknown>
  }
}

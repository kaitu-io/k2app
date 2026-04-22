import type { Config } from './config.js'

/**
 * Type alias for the fetch function signature, allowing injection of a custom
 * fetch implementation in tests.
 */
type FetchFn = (url: string | URL | Request, init?: RequestInit) => Promise<Response>

/**
 * Generic authenticated HTTP client. Used for both the Center (Go API) and the
 * CMS (Payload REST) targets.
 *
 * Automatically injects the X-Access-Key authentication header on every request.
 * Constructs full request URLs from the configured base URL.
 *
 * Use the `createCenterClient` / `createCmsClient` factories to build instances
 * from a full `Config` without repeating the field-plumbing boilerplate.
 *
 * @example
 * ```ts
 * const client = new CenterApiClient('https://api.kaitu.io', 'ktu_xxx')
 * const data = await client.request('/api/v1/users')
 * ```
 */
export class CenterApiClient {
  private readonly baseUrl: string
  private readonly accessKey: string
  private readonly fetchFn: FetchFn

  /**
   * Creates a new CenterApiClient.
   *
   * @param baseUrl - Base URL of the upstream service. A trailing slash is stripped.
   * @param accessKey - Value to send in the X-Access-Key authentication header.
   * @param fetchFn - Optional custom fetch function (defaults to global fetch). Used for testing.
   */
  constructor(baseUrl: string, accessKey: string, fetchFn: FetchFn = fetch) {
    this.baseUrl = baseUrl.replace(/\/$/, '')
    this.accessKey = accessKey
    this.fetchFn = fetchFn
  }

  /**
   * Makes an authenticated HTTP request to the configured base URL.
   *
   * Constructs the full URL by joining the configured base URL with the given path.
   * Automatically adds the X-Access-Key header for authentication, and a JSON
   * Content-Type by default. Callers may pass `{ 'Content-Type': '' }` to drop
   * the default — useful for multipart uploads where the runtime needs to
   * generate the boundary itself.
   *
   * On non-2xx responses, tries to preserve the upstream error body. Payload REST
   * returns `{ errors: [{ message, path? }] }` on 4xx/5xx; Center returns plain
   * text or `{ message }`. If the body isn't JSON, falls back to just the HTTP
   * status line.
   *
   * @param path - The API path to request (e.g. '/api/v1/users'). Leading slash optional.
   * @param options - Optional fetch RequestInit options (method, body, headers, etc.)
   * @returns Parsed JSON response body
   * @throws {Error} On network failure or non-2xx HTTP response
   */
  async request(path: string, options?: RequestInit): Promise<unknown> {
    const normalizedPath = path.startsWith('/') ? path : `/${path}`
    const url = `${this.baseUrl}${normalizedPath}`

    const headers: Record<string, string> = {
      'X-Access-Key': this.accessKey,
      'Content-Type': 'application/json',
      ...(options?.headers as Record<string, string> | undefined),
    }
    // Allow callers to opt out of default JSON Content-Type (e.g. multipart uploads
    // need the runtime to auto-generate a boundary).
    if (headers['Content-Type'] === '') {
      delete headers['Content-Type']
    }

    const response = await this.fetchFn(url, {
      ...options,
      headers,
    })

    if (!response.ok) {
      // Try to preserve the upstream error message. Payload REST returns
      // { errors: [{ message, path? }] } on 4xx; Center returns plain text or
      // { message }. Fall back to the HTTP status line if the body isn't JSON.
      let detail = ''
      try {
        const errBody = (await response.json()) as {
          errors?: Array<{ message?: string }>
          message?: string
        }
        if (errBody.errors && errBody.errors.length > 0 && errBody.errors[0]?.message) {
          detail = `: ${errBody.errors.map((e) => e.message).filter(Boolean).join('; ')}`
        } else if (errBody.message) {
          detail = `: ${errBody.message}`
        }
      } catch {
        // Body wasn't JSON — ignore, the HTTP status carries enough info.
      }
      throw new Error(
        `${options?.method ?? 'GET'} ${url} → HTTP ${response.status}${detail}`
      )
    }

    return response.json() as Promise<unknown>
  }
}

/** Creates a client targeting the Center API (Go backend). */
export function createCenterClient(config: Config, fetchFn?: FetchFn): CenterApiClient {
  return new CenterApiClient(config.center.url, config.center.accessKey, fetchFn)
}

/**
 * Creates a client targeting the CMS (Payload REST) using the same X-Access-Key
 * as the Center client. The CMS origin is independently configured via
 * `config.cms.url`; the access key comes from `config.center.accessKey`
 * because the CMS proxy accepts Center-issued credentials.
 */
export function createCmsClient(config: Config, fetchFn?: FetchFn): CenterApiClient {
  return new CenterApiClient(config.cms.url, config.center.accessKey, fetchFn)
}

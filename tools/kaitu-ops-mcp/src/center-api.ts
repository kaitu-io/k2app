// Stub — will be replaced in GREEN phase
import type { Config } from './config.ts'

export class CenterApiClient {
  constructor(
    _config: Config,
    _fetchFn?: (url: string | URL | Request, init?: RequestInit) => Promise<Response>
  ) {}

  async request(_path: string, _options?: RequestInit): Promise<unknown> {
    throw new Error('not implemented')
  }
}

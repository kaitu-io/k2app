/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_BASE_URL?: string
  readonly VITE_KAITU_ENTRY_URL?: string
  readonly VITE_USE_MOCK?: string
  readonly VITE_CLIENT_IS_ROUTER?: string
  readonly NODE_ENV: 'development' | 'production' | 'test'
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

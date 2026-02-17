/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_USE_MOCK: string
  readonly VITE_KAITU_ENTRY_URL: string
  readonly VITE_CLIENT_IS_ROUTER: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

/// <reference types="vite/client" />
/// <reference types="msw/vite/client" />

declare const __APP_VERSION__: string

interface ImportMetaEnv {
  readonly VITE_MOCK?: string
  readonly VITE_API_KEY?: string
  readonly VITE_ADMIN_TOKEN?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

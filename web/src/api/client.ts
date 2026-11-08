export class ApiError extends Error {
  readonly status: number
  readonly retryAfter: number | null

  constructor(status: number, message: string, retryAfter: number | null = null) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.retryAfter = retryAfter
  }
}

async function request<T>(token: string, method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Authorization: `Bearer ${token}` }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const res = await fetch(path, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    const retryAfter = Number(res.headers.get('Retry-After')) || null
    throw new ApiError(res.status, data?.error ?? res.statusText, retryAfter)
  }
  return data as T
}

function client(token: string) {
  return {
    get: <T>(path: string) => request<T>(token, 'GET', path),
    post: <T>(path: string, body?: unknown) => request<T>(token, 'POST', path, body),
    put: <T>(path: string, body: unknown) => request<T>(token, 'PUT', path, body),
  }
}

// It's a demo with no logins: the job routes take a team's API key and the
// admin routes one shared token, both from the environment.
export const adminToken = import.meta.env.VITE_ADMIN_TOKEN ?? ''

export const api = client(import.meta.env.VITE_API_KEY ?? '')

export const admin = client(adminToken)

export function describeError(err: unknown): string {
  if (err instanceof ApiError) {
    const wait = err.retryAfter ? ` Retry-After: ${err.retryAfter}s.` : ''
    return `Rejected with ${err.status}: ${err.message.replace(/\.$/, '')}.${wait}`
  }
  if (err instanceof TypeError) return 'Could not reach the API.'
  return err instanceof Error ? err.message : String(err)
}

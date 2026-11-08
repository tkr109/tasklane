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

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    const retryAfter = Number(res.headers.get('Retry-After')) || null
    throw new ApiError(res.status, data?.error ?? res.statusText, retryAfter)
  }
  return data as T
}

export const get = <T>(path: string) => request<T>('GET', path)

export const post = <T>(path: string, body?: unknown) => request<T>('POST', path, body)

export const patch = <T>(path: string, body: unknown) => request<T>('PATCH', path, body)

export function describeError(err: unknown): string {
  if (err instanceof ApiError) {
    const wait = err.retryAfter ? ` Retry-After: ${err.retryAfter}s.` : ''
    return `Rejected with ${err.status}: ${err.message.replace(/\.$/, '')}.${wait}`
  }
  if (err instanceof TypeError) return 'Could not reach the API.'
  return err instanceof Error ? err.message : String(err)
}

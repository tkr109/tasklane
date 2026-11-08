import { ApiError } from '../api/client'

interface QueryNoticeProps {
  error: Error | null
  what: string
  className?: string
}

// What a page shows while its data loads, or when it can't load.
export function QueryNotice({ error, what, className = 'card empty' }: QueryNoticeProps) {
  let text = `Loading ${what}…`
  if (error instanceof ApiError && error.status === 404) {
    text = `The API doesn't serve ${what} yet.`
  } else if (error instanceof ApiError && error.status === 401) {
    text = `The API turned down the dashboard's credentials (${error.message}). Check VITE_API_KEY and VITE_ADMIN_TOKEN.`
  } else if (error) {
    text = `Couldn't load ${what}: ${error.message}`
  }
  return <div className={className}>{text}</div>
}

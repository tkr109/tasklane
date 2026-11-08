import type { JobStatus } from '../api/types'

export function StatusPill({ status, label }: { status: JobStatus; label?: string }) {
  return <span className={`pill s-${status}`}>{label ?? status}</span>
}

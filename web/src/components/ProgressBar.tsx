import type { JobStatus } from '../api/types'

export function ProgressBar({ percent, status }: { percent: number; status: JobStatus }) {
  return (
    <div className={`bar s-${status}`}>
      <i style={{ width: `${percent}%` }} />
    </div>
  )
}

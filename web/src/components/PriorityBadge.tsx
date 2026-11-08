import { priorityBand } from '../lib/jobs'

export function PriorityBadge({ priority }: { priority: number }) {
  return <span className={`prio ${priorityBand(priority)}`}>{priority}</span>
}

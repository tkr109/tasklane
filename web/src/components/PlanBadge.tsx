import type { PlanName } from '../api/types'
import { capitalize } from '../lib/format'

export function PlanBadge({ plan }: { plan: PlanName }) {
  return <span className={`plan plan-${plan}`}>{capitalize(plan)}</span>
}

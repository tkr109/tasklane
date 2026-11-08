import type { Plan } from '../../api/types'
import { PlanBadge } from '../../components/PlanBadge'
import { formatNumber } from '../../lib/format'

export function PlanCards({ plans }: { plans: Plan[] }) {
  return (
    <div className="plans">
      {plans.map((plan) => (
        <div key={plan.name} className="card plan-card">
          <PlanBadge plan={plan.name} />
          <dl>
            <dt>Fair-share weight</dt>
            <dd>{plan.share_weight}</dd>
            <dt>Worker cap</dt>
            <dd>{plan.max_workers}</dd>
            <dt>Jobs per minute</dt>
            <dd>{formatNumber(plan.jobs_per_minute)}</dd>
            <dt>Burst</dt>
            <dd>{formatNumber(plan.burst)}</dd>
          </dl>
        </div>
      ))}
    </div>
  )
}

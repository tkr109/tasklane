import type { ScalerDecision } from '../../api/types'

const VERBS = { up: 'Scaled up', down: 'Scaled down' }

export function DecisionTitle({ decision: d }: { decision: ScalerDecision }) {
  const text = d.action === 'hold' ? `Holding at ${d.from}` : `${VERBS[d.action]} ${d.from} → ${d.to}`
  return <span className={`decision ${d.action}`}>{text}</span>
}

import type { ScalerDecision } from '../../api/types'
import { formatAgo, formatClock } from '../../lib/format'
import { DecisionTitle } from './DecisionTitle'

export function DecisionsTable({ decisions, now }: { decisions: ScalerDecision[]; now: number }) {
  if (decisions.length === 0) return <p className="card-note">The scaler hasn't changed anything yet.</p>
  return (
    <div className="tbl-wrap">
      <table className="tbl">
        <thead>
          <tr>
            <th>Time</th>
            <th>Decision</th>
            <th>Reason</th>
          </tr>
        </thead>
        <tbody>
          {decisions.slice(0, 30).map((d) => (
            <tr key={d.id}>
              <td className="num muted">
                {formatClock(d.at)} <span className="faint">· {formatAgo(d.at, now)}</span>
              </td>
              <td>
                <DecisionTitle decision={d} />
              </td>
              <td className="muted wrap">{d.reason}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

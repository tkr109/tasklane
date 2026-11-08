import type { Plan, PlanName, TeamRate, TeamSummary } from '../../api/types'
import { Meter } from '../../components/Meter'
import { PlanBadge } from '../../components/PlanBadge'
import { capitalize, formatAgo, formatNumber } from '../../lib/format'

interface TeamsTableProps {
  teams: TeamSummary[]
  plans: Plan[]
  rates: Record<number, TeamRate> | undefined
  pickedId: number | undefined
  now: number
  onPick: (id: number) => void
  onSwitchPlan: (team: TeamSummary, plan: PlanName) => void
}

function Usage({ team, rate }: { team: TeamSummary; rate: TeamRate | undefined }) {
  const limit = team.plan.jobs_per_minute
  const jobs = team.usage.jobs_last_minute
  return (
    <div className="usage">
      <div className="lbl">
        <span className="num">
          <b>{formatNumber(jobs)}</b> / {formatNumber(limit)}
        </span>
        <span className="muted">jobs in the last minute</span>
        {rate && rate.rejected_last_minute > 0 && (
          <span className="warn">{formatNumber(rate.rejected_last_minute)} got 429</span>
        )}
      </div>
      <Meter percent={(jobs / limit) * 100} />
    </div>
  )
}

export function TeamsTable({ teams, plans, rates, pickedId, now, onPick, onSwitchPlan }: TeamsTableProps) {
  return (
    <div className="tbl-wrap">
      <table className="tbl teams-table">
        <thead>
          <tr>
            <th>Team</th>
            <th>Usage</th>
            <th>Weight</th>
            <th>Fair share now</th>
            <th>Running</th>
            <th>Plan</th>
          </tr>
        </thead>
        <tbody>
          {teams.map((team) => {
            const rate = rates?.[team.id]
            return (
              <tr key={team.id} className={team.id === pickedId ? 'click picked' : 'click'} onClick={() => onPick(team.id)}>
                <td>
                  <div className="team-name">
                    {team.name}
                    <PlanBadge plan={team.plan.name} />
                  </div>
                  <div className="team-keys">created {formatAgo(team.created_at, now)}</div>
                </td>
                <td>
                  <Usage team={team} rate={rate} />
                </td>
                <td className="num">{team.plan.share_weight}</td>
                <td className="num">
                  {!rate ? (
                    <span className="faint">not reported yet</span>
                  ) : rate.fair_share ? (
                    `${Math.round(rate.fair_share * 100)}%`
                  ) : (
                    <span className="faint">idle</span>
                  )}
                </td>
                <td className="num">
                  {team.usage.running} <span className="muted">of cap {team.plan.max_workers}</span>
                </td>
                <td onClick={(e) => e.stopPropagation()}>
                  <select
                    className="select"
                    aria-label={`Plan for ${team.name}`}
                    value={team.plan.name}
                    onChange={(e) => onSwitchPlan(team, e.target.value as PlanName)}
                  >
                    {plans.map((p) => (
                      <option key={p.name} value={p.name}>
                        {capitalize(p.name)}
                      </option>
                    ))}
                  </select>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

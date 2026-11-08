import type { Plan, PlanName, Team } from '../../api/types'
import { Meter } from '../../components/Meter'
import { PlanBadge } from '../../components/PlanBadge'
import { capitalize, formatNumber } from '../../lib/format'

interface TeamsTableProps {
  teams: Team[]
  plans: Plan[]
  pickedId: number | undefined
  onPick: (id: number) => void
  onSwitchPlan: (team: Team, plan: PlanName) => void
}

function Usage({ team, plan }: { team: Team; plan: Plan }) {
  const percent = (team.jobs_this_minute / plan.jobs_per_minute) * 100
  return (
    <div className="usage">
      <div className="lbl">
        <span className="num">
          <b>{formatNumber(team.jobs_this_minute)}</b> / {formatNumber(plan.jobs_per_minute)}
        </span>
        <span className="muted">jobs this minute</span>
        {team.rejected_this_minute > 0 && (
          <span className="warn">{formatNumber(team.rejected_this_minute)} got 429</span>
        )}
      </div>
      <Meter percent={percent} />
    </div>
  )
}

export function TeamsTable({ teams, plans, pickedId, onPick, onSwitchPlan }: TeamsTableProps) {
  const planByName = new Map(plans.map((p) => [p.name, p]))

  return (
    <div className="tbl-wrap">
      <table className="tbl teams-table">
        <thead>
          <tr>
            <th>Team</th>
            <th>Usage</th>
            <th>Weight</th>
            <th>Fair share now</th>
            <th>Workers in use</th>
            <th>Plan</th>
          </tr>
        </thead>
        <tbody>
          {teams.map((team) => {
            const plan = planByName.get(team.plan)
            if (!plan) return null
            const activeKeys = team.keys.filter((k) => !k.revoked_at).length
            return (
              <tr key={team.id} className={team.id === pickedId ? 'click picked' : 'click'} onClick={() => onPick(team.id)}>
                <td>
                  <div className="team-name">
                    {team.name}
                    <PlanBadge plan={team.plan} />
                  </div>
                  <div className="team-keys">
                    {activeKeys} active key{activeKeys === 1 ? '' : 's'}
                  </div>
                </td>
                <td>
                  <Usage team={team} plan={plan} />
                </td>
                <td className="num">{plan.share_weight}</td>
                <td className="num">
                  {team.fair_share ? `${Math.round(team.fair_share * 100)}%` : <span className="faint">idle</span>}
                </td>
                <td className="num">
                  {team.workers_in_use} <span className="muted">of cap {plan.max_workers}</span>
                </td>
                <td onClick={(e) => e.stopPropagation()}>
                  <select
                    className="select"
                    aria-label={`Plan for ${team.name}`}
                    value={team.plan}
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

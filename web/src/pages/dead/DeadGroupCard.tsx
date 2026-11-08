import { useState } from 'react'
import type { DeadGroup } from '../../api/types'
import { formatAgo, formatNumber } from '../../lib/format'
import { GroupJobs } from './GroupJobs'
import { Sparkline } from './Sparkline'

interface DeadGroupCardProps {
  group: DeadGroup
  now: number
  leaving: boolean
  teamName: (id: number) => string
  onRetry: () => void
}

export function DeadGroupCard({ group, now, leaving, teamName, onRetry }: DeadGroupCardProps) {
  const [open, setOpen] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const count = formatNumber(group.count)

  return (
    <div className={leaving ? 'card group leaving' : 'card group'}>
      <div className="group-h">
        <div>
          <div>
            <span className="err">{group.pattern}</span> <span className="faint">·</span>{' '}
            <span className="cnt">{count} jobs</span>
          </div>
          {group.error !== group.pattern && (
            <div className="latest-err" title={group.error}>
              latest: <span className="mono">{group.error}</span>
            </div>
          )}
          <div className="group-meta">
            <span className="tag mono">{group.type}</span>
            <span>{group.teams.join(', ')}</span>
            <span>
              first {formatAgo(group.first_at, now)}, latest {formatAgo(group.last_at, now)}
            </span>
          </div>
        </div>
        <div className="group-actions">
          <Sparkline values={group.hourly} label="Jobs that died in each of the last 24 hours" />
          <button className="btn sm ghost" onClick={() => setOpen(!open)}>
            {open ? 'Hide jobs' : 'Show jobs'}
          </button>
          {confirming ? (
            <span className="confirm">
              Queue {count} jobs again?
              <button
                className="btn sm primary"
                onClick={() => {
                  setConfirming(false)
                  onRetry()
                }}
              >
                Retry {count}
              </button>
              <button className="btn sm ghost" onClick={() => setConfirming(false)}>
                Keep
              </button>
            </span>
          ) : (
            <button className="btn sm" onClick={() => setConfirming(true)}>
              Retry group
            </button>
          )}
        </div>
      </div>
      {open && <GroupJobs group={group} teamName={teamName} now={now} />}
    </div>
  )
}

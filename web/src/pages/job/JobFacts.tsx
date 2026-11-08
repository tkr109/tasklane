import type { ReactNode } from 'react'
import type { JobDetail, PlanName } from '../../api/types'
import { PlanBadge } from '../../components/PlanBadge'
import { PriorityBadge } from '../../components/PriorityBadge'
import { ProgressBar } from '../../components/ProgressBar'
import { formatAgo, formatClock, formatDuration, secondsUntil } from '../../lib/format'
import { priorityBand } from '../../lib/jobs'

interface JobFactsProps {
  job: JobDetail
  team?: { name: string; plan: PlanName }
  timeLimit?: number
  now: number
}

const PRIORITY_WORDS = { high: 'urgent', normal: 'normal', low: 'low' }

const none = <span className="faint">none</span>

export function JobFacts({ job, team, timeLimit, now }: JobFactsProps) {
  const runIn = secondsUntil(job.run_at, now)
  const facts: [string, ReactNode][] = [
    ['Type', <span className="mono">{job.type}</span>],
    [
      'Team',
      team ? (
        <>
          {team.name}
          <PlanBadge plan={team.plan} />
        </>
      ) : (
        `team ${job.team_id}`
      ),
    ],
    [
      'Priority',
      <>
        <PriorityBadge priority={job.priority} />{' '}
        <span className="muted hint">{PRIORITY_WORDS[priorityBand(job.priority)]}</span>
      </>,
    ],
    ['Attempts', <span className="num">{`${job.attempts} of ${job.max_attempts}`}</span>],
    [
      'Created',
      <>
        {formatClock(job.created_at)} <span className="muted">· {formatAgo(job.created_at, now)}</span>
      </>,
    ],
    ['Run at', runIn > 0 ? `in ${formatDuration(runIn)}` : formatClock(job.run_at)],
    ['Locked by', job.status === 'running' && job.locked_by ? <span className="mono">{job.locked_by}</span> : none],
    ['Lease until', job.status === 'running' && job.lease_until ? formatClock(job.lease_until) : none],
    ['Idempotency key', job.idempotency_key ? <span className="mono">{job.idempotency_key}</span> : none],
    ['Time limit', timeLimit ? `${timeLimit}s` : <span className="faint">unknown</span>],
    [
      'Finished',
      job.finished_at ? (
        <>
          {formatClock(job.finished_at)}{' '}
          <span className="muted">
            · {formatDuration(secondsUntil(job.finished_at, new Date(job.created_at).getTime()))} after submit
          </span>
        </>
      ) : (
        <span className="faint">not yet</span>
      ),
    ],
    [
      'Last error',
      job.last_error ? (
        <span className="mono error-text" title={job.last_error}>
          {job.last_error}
        </span>
      ) : (
        none
      ),
    ],
  ]

  return (
    <div className="card">
      <dl className="facts">
        {facts.map(([label, value]) => (
          <div key={label}>
            <dt>{label}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
      {job.status === 'running' && (
        <div className="big-progress">
          <span className="muted">Progress</span>
          <ProgressBar percent={Math.min(99, job.progress)} status="running" />
          <b className="num">{Math.min(99, Math.floor(job.progress))}%</b>
        </div>
      )}
    </div>
  )
}

import { Link } from 'react-router'
import type { Worker } from '../../api/types'
import { formatDuration, formatNumber, plural } from '../../lib/format'

// Workers beat every few seconds; one that's quiet for this long is worth a look.
const LATE_HEARTBEAT_SECONDS = 10

const byHost = (a: Worker, b: Worker) => a.host.localeCompare(b.host, 'en', { numeric: true })

// Enough chips for a typical worker; the rest are summed up.
const MAX_CHIPS = 4

function RunningJobs({ worker }: { worker: Worker }) {
  const jobs = worker.running_jobs
  if (worker.state === 'starting') return <span className="faint">container starting</span>
  if (jobs.length === 0) {
    return (
      <span className="faint">{worker.state === 'draining' ? 'finishing, takes no new jobs' : 'idle, waiting on notify'}</span>
    )
  }
  const shown = [...jobs].sort((a, b) => a.id - b.id).slice(0, MAX_CHIPS)
  return (
    <div className="running-jobs">
      <span className="num">{plural(jobs.length, 'job')}</span>
      {shown.map((job) => (
        <Link key={job.id} className="job-chip" to={`/jobs/${job.id}`} title={`Job ${job.id}, ${job.progress}%`}>
          <span className="mono">{job.type}</span>
          <span className="chip-bar">
            <i style={{ width: `${Math.min(100, job.progress)}%` }} />
          </span>
        </Link>
      ))}
      {jobs.length > shown.length && <span className="faint">+{jobs.length - shown.length} more</span>}
    </div>
  )
}

function Heartbeat({ lastSeen, now }: { lastSeen: string; now: number }) {
  const age = Math.max(0, (now - new Date(lastSeen).getTime()) / 1000)
  return (
    <span className={age > LATE_HEARTBEAT_SECONDS ? 'heartbeat late' : 'heartbeat'}>
      <i />
      {age.toFixed(1)}s ago
    </span>
  )
}

export function WorkersTable({ workers, now }: { workers: Worker[]; now: number }) {
  return (
    <div className="tbl-wrap">
      <table className="tbl">
        <thead>
          <tr>
            <th>Worker</th>
            <th>Container</th>
            <th>State</th>
            <th>Running</th>
            <th>Heartbeat</th>
            <th>Jobs done</th>
            <th>Up for</th>
          </tr>
        </thead>
        <tbody>
          {[...workers].sort(byHost).map((w) => {
            const upFor = (now - new Date(w.started_at).getTime()) / 1000
            const classes = [w.state === 'draining' && 'leaving', upFor < 1.6 && 'arriving'].filter(Boolean).join(' ')
            return (
              <tr key={w.id} className={classes || undefined}>
                <td className="mono">{w.id}</td>
                <td className="mono muted">{w.host}</td>
                <td>
                  <span className={`worker-state ${w.state}`}>
                    <i />
                    {w.state}
                  </span>
                </td>
                <td>
                  <RunningJobs worker={w} />
                </td>
                <td>
                  <Heartbeat lastSeen={w.last_seen_at} now={now} />
                </td>
                <td className="num">{formatNumber(w.jobs_done)}</td>
                <td className="num muted">
                  {w.state === 'starting' ? '—' : upFor < 1 ? 'just started' : formatDuration(upFor)}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

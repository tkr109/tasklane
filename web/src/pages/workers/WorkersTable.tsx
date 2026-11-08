import { Link } from 'react-router'
import type { Worker } from '../../api/types'
import { formatDuration, formatNumber } from '../../lib/format'

// Workers beat every few seconds; one that's quiet for this long is worth a look.
const LATE_HEARTBEAT_SECONDS = 10

const byHost = (a: Worker, b: Worker) => a.host.localeCompare(b.host, 'en', { numeric: true })

function CurrentJob({ worker }: { worker: Worker }) {
  if (worker.state === 'starting') return <span className="faint">container starting</span>
  if (worker.current_job) {
    return (
      <>
        <Link className="mono" to={`/jobs/${worker.current_job.id}`}>
          {worker.current_job.id}
        </Link>{' '}
        <span className="faint mono">{worker.current_job.type}</span>
      </>
    )
  }
  return (
    <span className="faint">{worker.state === 'draining' ? 'finishing, takes no new jobs' : 'idle, waiting on notify'}</span>
  )
}

function Heartbeat({ lastSeen, now }: { lastSeen: string | null; now: number }) {
  if (!lastSeen) return <span className="faint">none yet</span>
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
            <th>Current job</th>
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
                  <CurrentJob worker={w} />
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

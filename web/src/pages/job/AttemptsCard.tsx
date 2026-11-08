import type { JobDetail, JobStatus } from '../../api/types'
import { StatusPill } from '../../components/StatusPill'
import { formatClock, formatDuration, secondsUntil } from '../../lib/format'
import { attemptsFromTimeline, type Attempt } from '../../lib/jobs'

const OUTCOME_STATUS: Record<Attempt['outcome'], JobStatus> = {
  running: 'running',
  succeeded: 'succeeded',
  failed: 'failed',
  'lease expired': 'queued',
}

export function AttemptsCard({ job }: { job: JobDetail }) {
  const attempts = attemptsFromTimeline(job.timeline)
  return (
    <div className="card">
      <div className="card-h">
        <h2>Attempts</h2>
        <span className="sub">
          {job.attempts} of {job.max_attempts} used
        </span>
      </div>
      {attempts.length === 0 ? (
        <p className="card-note">No attempts yet.</p>
      ) : (
        <div className="tbl-wrap">
          <table className="tbl">
            <thead>
              <tr>
                <th>#</th>
                <th>Worker</th>
                <th>Started</th>
                <th>Took</th>
                <th>Outcome</th>
                <th>Error</th>
              </tr>
            </thead>
            <tbody>
              {attempts.map((a) => (
                <tr key={a.startedAt}>
                  <td className="num">{a.number}</td>
                  <td className="mono">{a.worker}</td>
                  <td className="num">{formatClock(a.startedAt)}</td>
                  <td className="num">
                    {a.endedAt ? (
                      formatDuration(secondsUntil(a.endedAt, new Date(a.startedAt).getTime()))
                    ) : (
                      <span className="muted">running</span>
                    )}
                  </td>
                  <td>
                    <StatusPill status={OUTCOME_STATUS[a.outcome]} label={a.outcome} />
                  </td>
                  <td className="mono error-text wrap">{a.error}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

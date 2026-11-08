import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import { deadGroupJobsQuery } from '../../api/queries'
import type { DeadGroup } from '../../api/types'
import { QueryNotice } from '../../components/QueryNotice'
import { formatAgo, formatNumber } from '../../lib/format'

export function GroupJobs({ group, teamName, now }: { group: DeadGroup; teamName: (id: number) => string; now: number }) {
  const navigate = useNavigate()
  const { data, error } = useQuery(deadGroupJobsQuery(group.id))

  if (!data) return <QueryNotice error={error} what="the jobs" className="card-note" />

  const more = group.count - data.jobs.length
  return (
    <div className="tbl-wrap">
      <table className="tbl">
        <thead>
          <tr>
            <th>Job</th>
            <th>Team</th>
            <th>Attempts</th>
            <th>Last error</th>
            <th>Died</th>
          </tr>
        </thead>
        <tbody>
          {data.jobs.map((job) => (
            <tr key={job.id} className="click" onClick={() => navigate(`/jobs/${job.id}`)}>
              <td className="mono">{job.id}</td>
              <td>{teamName(job.team_id)}</td>
              <td className="num">
                {job.attempts} / {job.max_attempts}
              </td>
              <td className="mono error-text">{job.last_error}</td>
              <td className="muted">{job.finished_at ? formatAgo(job.finished_at, now) : '—'}</td>
            </tr>
          ))}
          {more > 0 && (
            <tr>
              <td colSpan={5} className="muted">
                and {formatNumber(more)} more with the same error
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  )
}

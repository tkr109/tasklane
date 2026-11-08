import type { Job } from '../../api/types'
import { PriorityBadge } from '../../components/PriorityBadge'
import { ProgressBar } from '../../components/ProgressBar'
import { StatusPill } from '../../components/StatusPill'
import { formatAgo } from '../../lib/format'
import { progressLabel, progressPercent } from '../../lib/jobs'

interface JobRowProps {
  job: Job
  teamName: string
  selected: boolean
  now: number
  onToggle: (id: number) => void
  onOpen: (id: number) => void
}

export function JobRow({ job, teamName, selected, now, onToggle, onOpen }: JobRowProps) {
  return (
    <tr className={selected ? 'click selected' : 'click'} onClick={() => onOpen(job.id)}>
      <td
        className="cb"
        onClick={(e) => {
          e.stopPropagation()
          onToggle(job.id)
        }}
      >
        <input
          type="checkbox"
          aria-label={`Select job ${job.id}`}
          checked={selected}
          onChange={() => onToggle(job.id)}
          onClick={(e) => e.stopPropagation()}
        />
      </td>
      <td className="mono">{job.id}</td>
      <td className="mono">{job.type}</td>
      <td>{teamName}</td>
      <td>
        <PriorityBadge priority={job.priority} />
      </td>
      <td>
        <StatusPill status={job.status} />
      </td>
      <td>
        <div className="progress">
          <ProgressBar percent={progressPercent(job)} status={job.status} />
          <span className="pct mono">{progressLabel(job, now)}</span>
        </div>
      </td>
      <td className="mono">
        {job.attempts} / {job.max_attempts}
      </td>
      <td className="mono">{job.locked_by || '—'}</td>
      <td className="muted">{formatAgo(job.created_at, now)}</td>
    </tr>
  )
}

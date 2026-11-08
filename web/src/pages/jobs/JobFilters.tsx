import type { KeyboardEvent } from 'react'
import { useNavigate } from 'react-router'
import { JOB_STATUSES } from '../../api/types'
import { capitalize } from '../../lib/format'
import { PRIORITY_BANDS } from '../../lib/jobs'
import type { JobFilterState } from './useJobFilters'

interface JobFiltersProps {
  filters: JobFilterState
  types: string[]
  teams: { id: number; name: string }[]
  shown: number
  onChange: (patch: Partial<JobFilterState>) => void
  onClear: () => void
}

export function JobFilters({ filters, types, teams, shown, onChange, onClear }: JobFiltersProps) {
  const navigate = useNavigate()

  const openOnEnter = (e: KeyboardEvent<HTMLInputElement>) => {
    const id = filters.search.trim()
    if (e.key === 'Enter' && /^\d+$/.test(id)) navigate(`/jobs/${id}`)
  }

  return (
    <div className="filters">
      <input
        className="input mono search"
        placeholder="Search job id"
        aria-label="Search job id"
        title="Filters the loaded jobs. Press Enter to open a job by its id."
        value={filters.search}
        onChange={(e) => onChange({ search: e.target.value })}
        onKeyDown={openOnEnter}
      />
      <select
        className="select"
        aria-label="Status"
        value={filters.status}
        onChange={(e) => onChange({ status: e.target.value as JobFilterState['status'] })}
      >
        <option value="">All statuses</option>
        {JOB_STATUSES.map((s) => (
          <option key={s} value={s}>
            {capitalize(s)}
          </option>
        ))}
      </select>
      <select className="select" aria-label="Type" value={filters.type} onChange={(e) => onChange({ type: e.target.value })}>
        <option value="">All types</option>
        {types.map((t) => (
          <option key={t}>{t}</option>
        ))}
      </select>
      <select className="select" aria-label="Team" value={filters.team} onChange={(e) => onChange({ team: e.target.value })}>
        <option value="">All teams</option>
        {teams.map((t) => (
          <option key={t.id} value={t.id}>
            {t.name}
          </option>
        ))}
      </select>
      <select
        className="select"
        aria-label="Priority"
        value={filters.priority}
        onChange={(e) => onChange({ priority: e.target.value as JobFilterState['priority'] })}
      >
        <option value="">All priorities</option>
        {PRIORITY_BANDS.map((b) => (
          <option key={b.value} value={b.value}>
            {b.label}
          </option>
        ))}
      </select>
      <button className="btn ghost sm" onClick={onClear}>
        Clear filters
      </button>
      <span className="grow" />
      <span className="muted num count">{shown.toLocaleString('en-US')} shown</span>
    </div>
  )
}

import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { useCallback, useMemo, useState } from 'react'
import { useNavigate } from 'react-router'
import { listJobs } from '../../api/jobs'
import { queryKeys } from '../../api/keys'
import { statsQuery, teamsQuery } from '../../api/queries'
import type { Job } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { QueryNotice } from '../../components/QueryNotice'
import { useJobActions } from '../../hooks/useJobActions'
import { useNow } from '../../hooks/useNow'
import { priorityBand } from '../../lib/jobs'
import { BulkBar } from './BulkBar'
import { JobFilters } from './JobFilters'
import { JobRow } from './JobRow'
import { useJobFilters, type JobFilterState } from './useJobFilters'

function matches(job: Job, f: JobFilterState) {
  return (
    (!f.status || job.status === f.status) &&
    (!f.type || job.type === f.type) &&
    (!f.team || String(job.team_id) === f.team) &&
    (!f.priority || priorityBand(job.priority) === f.priority) &&
    (!f.search || String(job.id).includes(f.search.trim()))
  )
}

export function JobsPage() {
  const navigate = useNavigate()
  const now = useNow()
  const { filters, update, clear } = useJobFilters()
  const [selected, setSelected] = useState<Set<number>>(() => new Set())
  const actions = useJobActions()

  // Status and type are filtered by the API; team, priority and id search apply to the loaded rows.
  const serverFilters = useMemo(
    () => ({ status: filters.status || undefined, type: filters.type || undefined }),
    [filters.status, filters.type],
  )
  const jobsQuery = useInfiniteQuery({
    queryKey: queryKeys.jobList(serverFilters),
    queryFn: ({ pageParam }) => listJobs(serverFilters, pageParam),
    initialPageParam: '',
    getNextPageParam: (last) => last.next_cursor || undefined,
  })
  const { data: stats } = useQuery(statsQuery)
  const { data: teamsData } = useQuery(teamsQuery)

  const loaded = useMemo(() => jobsQuery.data?.pages.flatMap((p) => p.jobs) ?? [], [jobsQuery.data])
  const visible = useMemo(() => loaded.filter((job) => matches(job, filters)), [loaded, filters])

  const teamNames = useMemo(() => new Map(teamsData?.teams.map((t) => [t.id, t.name])), [teamsData])
  const teamName = (id: number) => teamNames.get(id) ?? `team ${id}`

  const typeOptions = useMemo(() => {
    const types = new Set(stats?.types.map((t) => t.type))
    loaded.forEach((job) => types.add(job.type))
    if (filters.type) types.add(filters.type)
    return [...types].sort()
  }, [stats, loaded, filters.type])

  const teamOptions = useMemo(() => {
    if (teamsData) return teamsData.teams.map(({ id, name }) => ({ id, name }))
    const ids = [...new Set(loaded.map((job) => job.team_id))].sort((a, b) => a - b)
    return ids.map((id) => ({ id, name: `team ${id}` }))
  }, [teamsData, loaded])

  const toggle = useCallback((id: number) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (!next.delete(id)) next.add(id)
      return next
    })
  }, [])
  const open = useCallback((id: number) => navigate(`/jobs/${id}`), [navigate])

  const allSelected = visible.length > 0 && visible.every((job) => selected.has(job.id))
  const someSelected = !allSelected && visible.some((job) => selected.has(job.id))
  const toggleAll = () => {
    setSelected((prev) => {
      const next = new Set(prev)
      visible.forEach((job) => (allSelected ? next.delete(job.id) : next.add(job.id)))
      return next
    })
  }

  const runBulk = async (action: 'retry' | 'cancel') => {
    const jobs = loaded.filter((job) => selected.has(job.id))
    setSelected(new Set())
    await actions[action](jobs)
  }

  return (
    <>
      <PageHeader title="Jobs" sub="Newest first. Status and progress update as workers report in." />
      <div className="card">
        <JobFilters
          filters={filters}
          types={typeOptions}
          teams={teamOptions}
          shown={visible.length}
          onChange={update}
          onClear={clear}
        />
        {selected.size > 0 && (
          <BulkBar
            count={selected.size}
            onRetry={() => void runBulk('retry')}
            onCancel={() => void runBulk('cancel')}
            onClear={() => setSelected(new Set())}
          />
        )}
        {jobsQuery.data ? (
          <div className="tbl-wrap">
            <table className="tbl jobs-table">
              <thead>
                <tr>
                  <th className="cb">
                    <input
                      type="checkbox"
                      aria-label="Select all"
                      checked={allSelected}
                      ref={(el) => {
                        if (el) el.indeterminate = someSelected
                      }}
                      onChange={toggleAll}
                    />
                  </th>
                  <th>Job</th>
                  <th>Type</th>
                  <th>Team</th>
                  <th>Priority</th>
                  <th>Status</th>
                  <th>Progress</th>
                  <th>Attempts</th>
                  <th>Worker</th>
                  <th>Created</th>
                </tr>
              </thead>
              <tbody>
                {visible.map((job) => (
                  <JobRow
                    key={job.id}
                    job={job}
                    teamName={teamName(job.team_id)}
                    selected={selected.has(job.id)}
                    now={now}
                    onToggle={toggle}
                    onOpen={open}
                  />
                ))}
              </tbody>
            </table>
            {visible.length === 0 && <div className="empty">No jobs match these filters.</div>}
          </div>
        ) : (
          <QueryNotice error={jobsQuery.error} what="jobs" className="empty" />
        )}
        <div className="tbl-foot">
          <span>Showing the {loaded.length.toLocaleString('en-US')} most recent jobs</span>
          <span className="grow" />
          <button
            className="btn sm"
            onClick={() => void jobsQuery.fetchNextPage()}
            disabled={!jobsQuery.hasNextPage || jobsQuery.isFetchingNextPage}
          >
            {jobsQuery.isFetchingNextPage ? 'Loading…' : jobsQuery.hasNextPage ? 'Load older' : 'No older jobs'}
          </button>
        </div>
      </div>
    </>
  )
}

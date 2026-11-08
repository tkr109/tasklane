import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router'
import { ApiError } from '../../api/client'
import { jobQuery, statsQuery, teamsQuery } from '../../api/queries'
import { BackIcon } from '../../components/icons'
import { JsonView } from '../../components/JsonView'
import { QueryNotice } from '../../components/QueryNotice'
import { StatusPill } from '../../components/StatusPill'
import { useJobActions } from '../../hooks/useJobActions'
import { useNow } from '../../hooks/useNow'
import { useToast } from '../../hooks/useToast'
import { canCancel, canRetry } from '../../lib/jobs'
import { AttemptsCard } from './AttemptsCard'
import { JobFacts } from './JobFacts'
import { ResultCard } from './ResultCard'
import { TimelineCard } from './TimelineCard'

export function JobDetailPage() {
  const id = Number(useParams().id)
  const validId = Number.isInteger(id) && id > 0
  const now = useNow()
  const toast = useToast()
  const actions = useJobActions()
  const { data: job, error } = useQuery({ ...jobQuery(id), enabled: validId })
  const { data: stats } = useQuery(statsQuery)
  const { data: teamsData } = useQuery(teamsQuery)

  const back = (
    <Link className="back" to="/jobs">
      <BackIcon />
      Back to jobs
    </Link>
  )

  if (!job) {
    const missing = !validId || (error instanceof ApiError && error.status === 404)
    return (
      <>
        {back}
        {missing ? <div className="card empty">There is no job with that id.</div> : <QueryNotice error={error} what="the job" />}
      </>
    )
  }

  const team = teamsData?.teams.find((t) => t.id === job.team_id)
  const timeLimit = stats?.types.find((t) => t.type === job.type)?.timeout_seconds

  const copyId = () => {
    navigator.clipboard.writeText(String(job.id)).then(
      () => toast('Job id copied.'),
      () => toast(`Job id: ${job.id}`),
    )
  }

  return (
    <>
      {back}
      <div className="page-header job-head">
        <h1>{job.id}</h1>
        <StatusPill status={job.status} />
        <span className="grow" />
        <button className="btn" onClick={copyId}>
          Copy id
        </button>
        {canCancel(job.status) && (
          <button className="btn danger" onClick={() => void actions.cancel([job])}>
            Cancel job
          </button>
        )}
        {canRetry(job.status) && (
          <button className="btn primary" onClick={() => void actions.retry([job])}>
            Retry job
          </button>
        )}
      </div>
      <JobFacts job={job} team={team} timeLimit={timeLimit} now={now} />
      <div className="detail-grid">
        <div className="stack">
          <div className="card">
            <div className="card-h">
              <h2>Payload</h2>
              <span className="sub">jsonb, as submitted</span>
            </div>
            <JsonView value={job.payload} />
          </div>
          <ResultCard job={job} />
          <AttemptsCard job={job} />
        </div>
        <TimelineCard events={job.timeline} />
      </div>
    </>
  )
}

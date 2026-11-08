import type { Job } from '../../api/types'
import { JsonView } from '../../components/JsonView'

const WAITING_TEXT: Record<Job['status'], string> = {
  queued: 'No result yet. The job is waiting for a worker.',
  running: 'No result yet. The job is running.',
  succeeded: 'The job finished without a result.',
  failed: 'No result yet. The next attempt is scheduled.',
  dead: 'No result. Every attempt failed.',
  cancelled: 'No result. The job was cancelled before it ran.',
}

export function ResultCard({ job }: { job: Job }) {
  const failed = job.status === 'failed' || job.status === 'dead'
  return (
    <div className="card">
      <div className="card-h">
        <h2>Result</h2>
      </div>
      {job.result !== undefined ? (
        <JsonView value={job.result} />
      ) : (
        <>
          {failed && job.last_error && <div className="err-box">{job.last_error}</div>}
          <p className="card-note">{WAITING_TEXT[job.status]}</p>
        </>
      )}
    </div>
  )
}

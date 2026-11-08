import { useQuery } from '@tanstack/react-query'
import { statsQuery, workersQuery } from '../../api/queries'
import { FloodButton } from '../../components/FloodButton'
import { PageHeader } from '../../components/PageHeader'
import { QueryNotice } from '../../components/QueryNotice'
import { useNow } from '../../hooks/useNow'
import { formatDuration, formatNumber } from '../../lib/format'
import { DecisionsTable } from './DecisionsTable'
import { WorkersTable } from './WorkersTable'

function Stat({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="card stat">
      <div className="k">{label}</div>
      <div className="v">{value}</div>
    </div>
  )
}

export function WorkersPage() {
  const now = useNow()
  const { data, error } = useQuery(workersQuery)
  const { data: stats } = useQuery(statsQuery)
  const scaler = stats?.scaler

  return (
    <>
      <PageHeader
        title="Workers"
        sub="Each worker is a container. It renews its leases and sends a heartbeat every few seconds."
      >
        <FloodButton />
      </PageHeader>

      {stats && (
        <div className="stat-row">
          <Stat label="Running" value={stats.workers.running} />
          <Stat label="Starting / draining" value={`${stats.workers.starting} / ${stats.workers.draining}`} />
          <Stat label="Target" value={stats.scaler.target} />
          <Stat label="Runnable jobs" value={formatNumber(stats.runnable)} />
          <Stat label="Oldest wait" value={formatDuration(stats.oldest_wait_seconds)} />
        </div>
      )}

      <div className="card workers-card">
        <div className="card-h">
          <h2>Live workers</h2>
          {scaler && (
            <span className="sub">
              min {scaler.min}, max {scaler.max}, {formatNumber(scaler.jobs_per_worker)} runnable jobs per worker
            </span>
          )}
        </div>
        {data ? <WorkersTable workers={data.workers} now={now} /> : <QueryNotice error={error} what="workers" className="empty" />}
      </div>

      <div className="card">
        <div className="card-h">
          <h2>Scaler decisions</h2>
          {scaler && <span className="sub">Checked every {scaler.interval_seconds} seconds. Every change is logged.</span>}
        </div>
        {data ? (
          <DecisionsTable decisions={data.decisions} now={now} />
        ) : (
          <QueryNotice error={error} what="scaler decisions" className="empty" />
        )}
      </div>
    </>
  )
}

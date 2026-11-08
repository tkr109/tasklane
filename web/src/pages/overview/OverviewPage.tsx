import { useQuery } from '@tanstack/react-query'
import { statsQuery, workersQuery } from '../../api/queries'
import { FloodButton } from '../../components/FloodButton'
import { PageHeader } from '../../components/PageHeader'
import { QueryNotice } from '../../components/QueryNotice'
import { useFlood } from '../../hooks/useFlood'
import { useNow } from '../../hooks/useNow'
import { FLOOD_BATCH, FLOOD_JOB, FLOOD_SIZE } from '../../lib/flood'
import { formatNumber } from '../../lib/format'
import { AutoscalerCard } from './AutoscalerCard'
import { CountTiles } from './CountTiles'
import { JobTypesCard } from './JobTypesCard'
import { QueueChart } from './QueueChart'
import { ThroughputChart } from './ThroughputChart'

const average = (values: number[]) => values.reduce((sum, v) => sum + v, 0) / Math.max(1, values.length)

export function OverviewPage() {
  const now = useNow()
  const { startedAt } = useFlood()
  const { data: stats, error } = useQuery(statsQuery)
  const { data: workers } = useQuery(workersQuery)

  const header = <PageHeader title="Overview" sub="What the queue and the workers are doing right now." />
  if (!stats) {
    return (
      <>
        {header}
        <QueryNotice error={error} what="stats" />
      </>
    )
  }

  const latest = stats.samples.at(-1)
  const recent = stats.samples.slice(-5)

  return (
    <>
      {header}
      <CountTiles stats={stats} />

      <div className="grid-2 stretch">
        <div className="card queue-card">
          <div className="card-h">
            <div>
              <h2>Queue and workers</h2>
              <div className="legend">
                <span>
                  <i className="swatch" style={{ background: 'var(--accent)' }} />
                  Jobs waiting <b>{formatNumber(latest?.queued ?? 0)}</b>
                </span>
                <span>
                  <i className="swatch" style={{ background: 'var(--workers)' }} />
                  Workers running <b>{latest?.workers ?? 0}</b>
                </span>
              </div>
            </div>
            <span className="grow" />
            <span className="sub">Last 2.5 minutes</span>
            <FloodButton primary />
          </div>
          <div className="card-b chart-body">
            <QueueChart samples={stats.samples} maxWorkers={stats.scaler?.max} floodAt={startedAt} />
          </div>
        </div>
        <AutoscalerCard stats={stats} decisions={workers?.decisions} now={now} />
      </div>

      <div className="grid-2">
        <div className="card">
          <div className="card-h">
            <div>
              <h2>Throughput</h2>
              <div className="legend">
                <span>
                  <i className="swatch box" style={{ background: 'var(--ok)' }} />
                  Succeeded per second <b>{formatNumber(average(recent.map((s) => s.succeeded)))}</b>
                </span>
                <span>
                  <i className="swatch box" style={{ background: 'var(--failed)' }} />
                  Failed per second <b>{average(recent.map((s) => s.failed)).toFixed(1)}</b>
                </span>
              </div>
            </div>
            <span className="grow" />
            <span className="sub">Last 60 seconds</span>
          </div>
          <div className="card-b chart-body">
            <ThroughputChart samples={stats.samples} />
          </div>
        </div>
        <JobTypesCard types={stats.types} />
      </div>

      <p className="foot-note">
        Submits get a 503 while more than {formatNumber(stats.queue_ceiling)} jobs are queued. "Flood 10k jobs"
        submits {formatNumber(FLOOD_SIZE)} {FLOOD_JOB.type} jobs in batches of {FLOOD_BATCH}.
      </p>
    </>
  )
}

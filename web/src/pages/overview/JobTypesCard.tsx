import type { JobTypeStats } from '../../api/types'
import { Meter } from '../../components/Meter'
import { formatNumber, formatRate } from '../../lib/format'

function TypeRow({ type }: { type: JobTypeStats }) {
  const limit = type.outgoing_per_second
  const percent = limit ? Math.min(100, (type.rate_per_second / limit) * 100) : 0

  let note = limit ? `${Math.round(percent)}% of limit` : 'no outgoing limit'
  if (type.held > 0) note = `${formatNumber(type.held)} waiting on the limit`

  return (
    <div className="row">
      <div>
        <span className="mono name">{type.type}</span>
        <div className="meta">
          time limit {type.timeout_seconds}s · outgoing {limit ? `${formatNumber(limit)}/s` : 'none'}
        </div>
      </div>
      <div className="rate">
        <b className="num">{formatRate(type.rate_per_second)}</b>
        <span className="muted">/s</span>
        <div className={type.held > 0 ? 'meta held' : 'meta'}>{note}</div>
      </div>
      {limit !== null && <Meter percent={percent} />}
    </div>
  )
}

export function JobTypesCard({ types }: { types: JobTypeStats[] }) {
  return (
    <div className="card">
      <div className="card-h">
        <h2>Job types</h2>
        <span className="grow" />
        <span className="sub">Outgoing limits are shared by all workers</span>
      </div>
      <div className="card-b types">
        {types.map((t) => (
          <TypeRow key={t.type} type={t} />
        ))}
      </div>
    </div>
  )
}

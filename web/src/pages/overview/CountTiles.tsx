import type { CSSProperties } from 'react'
import { Link } from 'react-router'
import type { Stats } from '../../api/types'
import { formatDuration, formatNumber, plural } from '../../lib/format'

interface TileProps {
  to: string
  color: string
  label: string
  value: number
  detail: string
}

function Tile({ to, color, label, value, detail }: TileProps) {
  return (
    <Link className="card tile" to={to} style={{ '--c': `var(${color})` } as CSSProperties}>
      <div className="k">
        <i />
        {label}
      </div>
      <div className="v">{formatNumber(value)}</div>
      <div className="s">{detail}</div>
    </Link>
  )
}

export function CountTiles({ stats }: { stats: Stats }) {
  const { counts, workers } = stats
  const recent = stats.samples.slice(-10)
  const perMinute = (recent.reduce((sum, s) => sum + s.succeeded, 0) / Math.max(1, recent.length)) * 60

  return (
    <div className="tiles">
      <Tile
        to="/jobs?status=queued"
        color="--queued"
        label="Queued"
        value={counts.queued}
        detail={`oldest wait ${formatDuration(stats.oldest_wait_seconds)}`}
      />
      <Tile
        to="/jobs?status=running"
        color="--running"
        label="Running"
        value={counts.running}
        detail={`on ${plural(workers.running, 'worker')}`}
      />
      <Tile
        to="/jobs?status=succeeded"
        color="--ok"
        label="Succeeded today"
        value={counts.succeeded_today}
        detail={`${formatNumber(perMinute)} per minute now`}
      />
      <Tile
        to="/jobs?status=failed"
        color="--failed"
        label="Failed, retrying"
        value={counts.failed}
        detail="waiting for their next attempt"
      />
      <Tile
        to="/dead"
        color="--dead"
        label="Dead"
        value={counts.dead}
        detail={`in ${plural(stats.dead_groups, 'error group')}`}
      />
    </div>
  )
}

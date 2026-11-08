import {
  Area,
  CartesianGrid,
  ComposedChart,
  Line,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
  type DotItemDotProps,
  type TooltipContentProps,
} from 'recharts'
import type { Sample } from '../../api/types'
import { formatClock, formatNumber, formatShort } from '../../lib/format'

interface Point {
  at: number
  queued: number
  workers: number
}

interface QueueChartProps {
  samples: Sample[]
  // The scaler's max; without a scaler the axis fits the samples.
  maxWorkers: number | undefined
  floodAt: number | null
}

const TICK_EVERY_MS = 30_000

function QueueTooltip({ active, payload }: TooltipContentProps) {
  const point = payload?.[0]?.payload as Point | undefined
  if (!active || !point) return null
  return (
    <div className="chart-tip">
      <div className="muted">{formatClock(point.at)}</div>
      <div>
        <i className="swatch" style={{ background: 'var(--accent)' }} />
        Jobs waiting<b>{formatNumber(point.queued)}</b>
      </div>
      <div>
        <i className="swatch" style={{ background: 'var(--workers)' }} />
        Workers running<b>{point.workers}</b>
      </div>
    </div>
  )
}

export function QueueChart({ samples, maxWorkers, floodAt }: QueueChartProps) {
  const data: Point[] = samples.map((s) => ({ at: new Date(s.at).getTime(), queued: s.queued, workers: s.workers }))
  const first = data[0]?.at ?? 0
  const last = data.at(-1)?.at ?? 0
  const ticks: number[] = []
  for (let t = last; t >= first; t -= TICK_EVERY_MS) ticks.unshift(t)
  const workerMax = maxWorkers ?? Math.max(5, Math.ceil(Math.max(...data.map((p) => p.workers)) / 5) * 5)
  const workerTicks = Array.from({ length: 6 }, (_, i) => (workerMax * i) / 5)

  // Only mark the points where the worker count changed.
  const changeDot = ({ cx, cy, index }: DotItemDotProps) =>
    index > 0 && data[index].workers !== data[index - 1].workers ? (
      <circle key={index} cx={cx} cy={cy} r={3} fill="var(--panel)" stroke="var(--workers)" strokeWidth={2} />
    ) : null

  return (
    <div className="chart queue-chart">
      <ResponsiveContainer width="100%" height="100%">
        <ComposedChart data={data} margin={{ top: 8, right: 4, bottom: 0, left: 0 }}>
          <defs>
            <linearGradient id="queue-fill" x1="0" x2="0" y1="0" y2="1">
              <stop offset="0" stopColor="var(--accent)" stopOpacity={0.22} />
              <stop offset="1" stopColor="var(--accent)" stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid vertical={false} stroke="var(--grid)" />
          <XAxis
            dataKey="at"
            type="number"
            domain={['dataMin', 'dataMax']}
            ticks={ticks}
            tickFormatter={(t: number) => (t === last ? 'now' : formatClock(t))}
            tickLine={false}
            axisLine={{ stroke: 'var(--border)' }}
          />
          <YAxis yAxisId="queue" width={42} tickFormatter={formatShort} tickLine={false} axisLine={false} />
          <YAxis
            yAxisId="workers"
            orientation="right"
            width={30}
            domain={[0, workerMax]}
            ticks={workerTicks}
            tickLine={false}
            axisLine={false}
          />
          <Tooltip content={QueueTooltip} cursor={{ stroke: 'var(--muted)' }} isAnimationActive={false} />
          <Area
            yAxisId="queue"
            dataKey="queued"
            type="linear"
            stroke="var(--accent)"
            strokeWidth={2}
            fill="url(#queue-fill)"
            isAnimationActive={false}
            activeDot={{ r: 3.5, fill: 'var(--panel)', stroke: 'var(--accent)', strokeWidth: 2 }}
          />
          <Line
            yAxisId="workers"
            dataKey="workers"
            type="stepAfter"
            stroke="var(--workers)"
            strokeWidth={2.25}
            dot={changeDot}
            isAnimationActive={false}
            activeDot={{ r: 3.5, fill: 'var(--panel)', stroke: 'var(--workers)', strokeWidth: 2 }}
          />
          {floodAt !== null && floodAt > first && (
            <ReferenceLine
              yAxisId="queue"
              x={floodAt}
              stroke="var(--faint)"
              strokeDasharray="3 4"
              label={{ value: 'Flood 10k', position: 'insideBottomRight', fill: 'var(--muted)', fontSize: 11, fontWeight: 500 }}
            />
          )}
        </ComposedChart>
      </ResponsiveContainer>
    </div>
  )
}

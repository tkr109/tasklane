import { Bar, BarChart, CartesianGrid, ResponsiveContainer, XAxis, YAxis } from 'recharts'
import type { Sample } from '../../api/types'
import { formatShort } from '../../lib/format'

const WINDOW = 60

export function ThroughputChart({ samples }: { samples: Sample[] }) {
  // Always 60 bars, empty where there is no sample yet, so bars keep their width on a fresh API.
  const data = Array.from({ length: WINDOW }, (_, i) => {
    const secondsAgo = WINDOW - 1 - i
    const sample = samples.at(-1 - secondsAgo)
    return { secondsAgo, succeeded: sample?.succeeded, failed: sample?.failed }
  })
  const labels: Record<number, string> = { [WINDOW - 1]: '60s ago', 30: '30s ago', 0: 'now' }

  return (
    <div className="chart">
      <ResponsiveContainer width="100%" height={190}>
        <BarChart data={data} margin={{ top: 10, right: 14, bottom: 0, left: 0 }} barCategoryGap="22%">
          <CartesianGrid vertical={false} stroke="var(--grid)" />
          <XAxis
            dataKey="secondsAgo"
            ticks={Object.keys(labels).map(Number)}
            interval={0}
            tickFormatter={(s: number) => labels[s] ?? ''}
            tickLine={false}
            axisLine={{ stroke: 'var(--border)' }}
          />
          <YAxis width={42} tickFormatter={formatShort} tickLine={false} axisLine={false} />
          <Bar dataKey="succeeded" stackId="done" fill="var(--ok)" radius={1.5} isAnimationActive={false} />
          <Bar dataKey="failed" stackId="done" fill="var(--failed)" radius={1} isAnimationActive={false} />
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}

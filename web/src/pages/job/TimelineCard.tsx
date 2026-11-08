import type { JobEvent } from '../../api/types'
import { formatClock } from '../../lib/format'
import { EVENT_LABELS, eventMessage } from '../../lib/jobs'

const FILLED_KINDS = new Set(['created', 'succeeded', 'dead'])

export function TimelineCard({ events }: { events: JobEvent[] }) {
  return (
    <div className="card">
      <div className="card-h">
        <h2>Timeline</h2>
        <span className="sub">{events.length === 1 ? '1 event' : `${events.length} events`}</span>
      </div>
      <ul className="timeline">
        {events.map((event, i) => {
          const filled = i === events.length - 1 || FILLED_KINDS.has(event.kind)
          const message = eventMessage(event)
          return (
            <li key={event.id} className={`k-${event.kind}${filled ? ' filled' : ''}`}>
              <span className="dot" />
              <div className="h">
                <b>{EVENT_LABELS[event.kind] ?? event.kind}</b>
                <span>{formatClock(event.at)}</span>
              </div>
              {message && <p>{message}</p>}
            </li>
          )
        })}
      </ul>
    </div>
  )
}

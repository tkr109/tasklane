import type { CSSProperties } from 'react'
import { Link } from 'react-router'
import type { ScalerDecision, ScalerStatus, Stats, WorkerState } from '../../api/types'
import { formatClock, formatNumber, plural, secondsUntil } from '../../lib/format'
import { DecisionTitle } from '../workers/DecisionTitle'

interface AutoscalerCardProps {
  stats: Stats
  decisions: ScalerDecision[] | undefined
  now: number
}

const BUSY_QUEUE = 1000

function scalerState(
  { workers, counts }: Stats,
  scaler: ScalerStatus | null,
  now: number,
): { tone: string; text: string } {
  if (!scaler) return { tone: 'off', text: 'Scaler not running. The workers that are up keep working.' }
  if (workers.starting) {
    return { tone: 'up', text: `Starting ${plural(workers.starting, 'worker container')}` }
  }
  if (workers.draining) {
    return { tone: 'cool', text: 'Draining 1 worker: finishing its jobs, then stopping' }
  }
  if (scaler.scale_down_at && scaler.current > scaler.target) {
    const left = secondsUntil(scaler.scale_down_at, now)
    return {
      tone: 'cool',
      text:
        left > 0
          ? `Cool-down: scaling down in ${Math.ceil(left)}s`
          : `Scaling down one worker every ${scaler.step_down_seconds}s`,
    }
  }
  if (counts.queued > BUSY_QUEUE) {
    return { tone: 'up', text: `Draining the queue with ${plural(scaler.current, 'worker')}` }
  }
  return { tone: 'steady', text: `Steady at ${scaler.current}` }
}

export function AutoscalerCard({ stats, decisions, now }: AutoscalerCardProps) {
  const { scaler, workers } = stats
  const pips: (WorkerState | 'unused')[] = [
    ...Array<WorkerState>(workers.running).fill('running'),
    ...Array<WorkerState>(workers.draining).fill('draining'),
    ...Array<WorkerState>(workers.starting).fill('starting'),
  ]
  // Without a scaler there is no max: the blocks keep their usual size and there are no empty slots.
  const slots = scaler?.max ?? Math.max(pips.length, 10)
  while (scaler && pips.length < slots) pips.push('unused')
  const state = scalerState(stats, scaler, now)

  return (
    <div className="card scaler">
      <div className="card-h">
        <h2>Autoscaler</h2>
        <span className="grow" />
        <Link className="link" to="/workers">
          All decisions
        </Link>
      </div>
      <div className="card-b">
        <div className="big">
          <b>{workers.running}</b>
          <span className="muted">workers running</span>
          <span className="grow" />
          {scaler && <span className="muted num">target {scaler.target}</span>}
        </div>
        <div className="pips" style={{ '--slots': slots } as CSSProperties}>
          {pips.slice(0, slots).map((pip, i) => (
            <div key={i} className={`pip ${pip}`} title={pip === 'unused' ? 'unused slot' : pip} />
          ))}
        </div>
        <div className="pip-scale">
          <span>One block per worker container</span>
          {scaler && <span>max {scaler.max}</span>}
        </div>
        <div className={`scaler-state ${state.tone}`}>
          <i />
          <span>{state.text}</span>
        </div>
        {scaler ? (
          <p className="rule">
            Target is runnable jobs divided by {formatNumber(scaler.jobs_per_worker)}, kept between {scaler.min} and{' '}
            {scaler.max}. Jobs held only by an outgoing rate limit are not counted. It scales up at once and scales
            down one worker at a time after a {scaler.cooldown_seconds}s cool-down.
          </p>
        ) : (
          <p className="rule">
            Once the scaler runs, it sets the worker count from the runnable jobs and logs each change here. Until
            then, workers are started and stopped by hand.
          </p>
        )}
        <h3>Recent decisions</h3>
        {decisions?.length ? (
          <ul className="decisions">
            {decisions.slice(0, 3).map((d) => (
              <li key={d.id} className={now - new Date(d.at).getTime() < 1500 ? 'fresh' : undefined}>
                <span className="t">{formatClock(d.at)}</span>
                <div>
                  <DecisionTitle decision={d} />
                  <div className="why">{d.reason}</div>
                </div>
              </li>
            ))}
          </ul>
        ) : (
          <p className="rule">No decisions to show yet.</p>
        )}
      </div>
    </div>
  )
}

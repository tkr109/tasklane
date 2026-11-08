import { useState, type FormEvent } from 'react'
import type { ScheduleSpec } from '../../api/types'
import { FLOOD_JOB } from '../../lib/flood'

const EXAMPLE_PAYLOADS: Record<string, unknown> = {
  sleep: { seconds: 5 },
  flaky: { fail_rate: 0.3 },
  'email.send': { to: 'ops@example.com', template: 'welcome', data: { name: 'Ops' } },
  'image.resize': FLOOD_JOB.payload,
  webhook: { url: 'https://example.com/hooks/tasklane', body: { event: 'ping' } },
}

const examplePayload = (type: string) => JSON.stringify(EXAMPLE_PAYLOADS[type] ?? {}, null, 2)

interface ScheduleFormProps {
  types: string[]
  saving: boolean
  error: string | null
  onCreate: (spec: ScheduleSpec) => void
}

export function ScheduleForm({ types, saving, error, onCreate }: ScheduleFormProps) {
  const [cron, setCron] = useState('*/5 * * * *')
  const [timezone, setTimezone] = useState(() => Intl.DateTimeFormat().resolvedOptions().timeZone)
  const [type, setType] = useState('sleep')
  const [payload, setPayload] = useState(() => examplePayload('sleep'))
  const [priority, setPriority] = useState(5)
  const [payloadError, setPayloadError] = useState<string | null>(null)

  const pickType = (next: string) => {
    // Swap the example only while the payload is still the old type's example.
    if (payload === examplePayload(type)) setPayload(examplePayload(next))
    setType(next)
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    let parsed: unknown
    try {
      parsed = payload.trim() ? JSON.parse(payload) : undefined
    } catch {
      setPayloadError('The payload is not valid JSON.')
      return
    }
    setPayloadError(null)
    onCreate({ cron: cron.trim(), timezone: timezone.trim() || undefined, type, payload: parsed, priority })
  }

  return (
    <form className="schedule-form" onSubmit={submit}>
      <label>
        <span>Cron</span>
        <input className="input mono" value={cron} onChange={(e) => setCron(e.target.value)} required />
        <small>Five fields or a descriptor such as @hourly. At most once a minute.</small>
      </label>
      <label>
        <span>Time zone</span>
        <input className="input" value={timezone} onChange={(e) => setTimezone(e.target.value)} placeholder="UTC" />
      </label>
      <label>
        <span>Job type</span>
        <select className="select" value={type} onChange={(e) => pickType(e.target.value)}>
          {types.map((t) => (
            <option key={t}>{t}</option>
          ))}
        </select>
      </label>
      <label>
        <span>Priority</span>
        <input
          className="input"
          type="number"
          min={0}
          max={9}
          value={priority}
          onChange={(e) => setPriority(Number(e.target.value))}
        />
      </label>
      <label className="wide">
        <span>Payload</span>
        <textarea className="input mono" rows={5} value={payload} onChange={(e) => setPayload(e.target.value)} />
      </label>
      <div className="wide form-foot">
        {(payloadError ?? error) && <span className="error-text">{payloadError ?? error}</span>}
        <span className="grow" />
        <button className="btn primary" type="submit" disabled={saving}>
          {saving ? 'Creating…' : 'Create schedule'}
        </button>
      </div>
    </form>
  )
}

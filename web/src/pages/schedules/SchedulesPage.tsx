import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { describeError } from '../../api/client'
import { queryKeys } from '../../api/keys'
import { schedulesQuery, statsQuery } from '../../api/queries'
import { createSchedule, deleteSchedule, updateSchedule } from '../../api/schedules'
import type { Schedule } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { QueryNotice } from '../../components/QueryNotice'
import { useNow } from '../../hooks/useNow'
import { useToast } from '../../hooks/useToast'
import { formatAgo, formatClock, formatDuration, secondsUntil } from '../../lib/format'
import { ScheduleForm } from './ScheduleForm'

// What the API registers, for when stats aren't loaded.
const FALLBACK_TYPES = ['email.send', 'flaky', 'image.resize', 'sleep', 'webhook']

function NextRun({ schedule, now }: { schedule: Schedule; now: number }) {
  if (!schedule.enabled) return <span className="faint">paused</span>
  const wait = secondsUntil(schedule.next_run_at, now)
  return (
    <>
      {formatClock(schedule.next_run_at)}{' '}
      <span className="muted">· {wait > 0 ? `in ${formatDuration(wait)}` : 'due now'}</span>
    </>
  )
}

export function SchedulesPage() {
  const queryClient = useQueryClient()
  const toast = useToast()
  const now = useNow()
  const { data, error } = useQuery({ ...schedulesQuery, refetchInterval: 15_000 })
  const { data: stats } = useQuery(statsQuery)
  const [confirming, setConfirming] = useState<number | null>(null)

  const create = useMutation({
    mutationFn: createSchedule,
    onSuccess: (schedule) => {
      queryClient.setQueryData<{ schedules: Schedule[] }>(
        queryKeys.schedules,
        (prev) => prev && { schedules: [...prev.schedules, schedule] },
      )
      toast(`Schedule ${schedule.id} created. It first runs at ${formatClock(schedule.next_run_at)}.`)
    },
  })

  // The PUT replaces the whole schedule, so it sends the rest back unchanged.
  const toggle = useMutation({
    mutationFn: ({ id, cron, timezone, type, payload, priority, max_attempts, enabled }: Schedule) =>
      updateSchedule(id, { cron, timezone, type, payload, priority, max_attempts, enabled: !enabled }),
    onSuccess: (schedule) => {
      queryClient.setQueryData<{ schedules: Schedule[] }>(
        queryKeys.schedules,
        (prev) => prev && { schedules: prev.schedules.map((s) => (s.id === schedule.id ? schedule : s)) },
      )
      toast(
        schedule.enabled
          ? `Schedule ${schedule.id} resumed. It next runs at ${formatClock(schedule.next_run_at)}.`
          : `Schedule ${schedule.id} paused. It queues nothing until you resume it.`,
      )
    },
    onError: (err) => toast(describeError(err)),
  })

  const remove = useMutation({
    mutationFn: (schedule: Schedule) => deleteSchedule(schedule.id),
    onSuccess: (_, schedule) => {
      queryClient.setQueryData<{ schedules: Schedule[] }>(
        queryKeys.schedules,
        (prev) => prev && { schedules: prev.schedules.filter((s) => s.id !== schedule.id) },
      )
      toast(`Schedule ${schedule.id} deleted. Jobs it already queued stay in the queue.`)
    },
    onError: (err) => toast(describeError(err)),
  })

  const types = stats?.types.map((t) => t.type).sort() ?? FALLBACK_TYPES

  return (
    <>
      <PageHeader
        title="Schedules"
        sub="Repeating jobs for the API key's team. Each run queues one job from the template, once, even with several API processes."
      />

      <div className="card schedules-card">
        <div className="card-h">
          <h2>New schedule</h2>
        </div>
        <ScheduleForm
          types={types}
          saving={create.isPending}
          error={create.error ? describeError(create.error) : null}
          onCreate={(spec) => create.mutate(spec)}
        />
      </div>

      <div className="card">
        <div className="card-h">
          <h2>Schedules</h2>
          {data && <span className="sub">{data.schedules.length} of at most 50</span>}
        </div>
        {!data ? (
          <QueryNotice error={error} what="schedules" className="empty" />
        ) : data.schedules.length === 0 ? (
          <p className="card-note">No schedules yet.</p>
        ) : (
          <div className="tbl-wrap">
            <table className="tbl">
              <thead>
                <tr>
                  <th>Id</th>
                  <th>Cron</th>
                  <th>Type</th>
                  <th>Priority</th>
                  <th>Next run</th>
                  <th>Last run</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {data.schedules.map((s) => (
                  <tr key={s.id}>
                    <td className="mono">{s.id}</td>
                    <td>
                      <span className="mono">{s.cron}</span> <span className="faint">{s.timezone}</span>
                    </td>
                    <td className="mono">{s.type}</td>
                    <td className="num">{s.priority}</td>
                    <td className="num">
                      <NextRun schedule={s} now={now} />
                    </td>
                    <td className="muted">{s.last_run_at ? formatAgo(s.last_run_at, now) : 'never'}</td>
                    <td className="actions">
                      {confirming === s.id ? (
                        <span className="confirm">
                          Delete it?
                          <button
                            className="btn sm danger"
                            onClick={() => {
                              setConfirming(null)
                              remove.mutate(s)
                            }}
                          >
                            Delete
                          </button>
                          <button className="btn sm ghost" onClick={() => setConfirming(null)}>
                            Keep
                          </button>
                        </span>
                      ) : (
                        <>
                          <button
                            className="btn sm ghost"
                            disabled={toggle.isPending && toggle.variables.id === s.id}
                            onClick={() => toggle.mutate(s)}
                          >
                            {s.enabled ? 'Pause' : 'Resume'}
                          </button>
                          <button className="btn sm ghost" onClick={() => setConfirming(s.id)}>
                            Delete
                          </button>
                        </>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </>
  )
}

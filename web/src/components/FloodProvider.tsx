import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useMemo, useRef, useState, type ReactNode } from 'react'
import { describeError } from '../api/client'
import { submitBatch } from '../api/jobs'
import { queryKeys } from '../api/keys'
import { FloodContext } from '../hooks/useFlood'
import { useToast } from '../hooks/useToast'
import { FLOOD_BATCH, FLOOD_JOB, FLOOD_SIZE } from '../lib/flood'
import { formatNumber } from '../lib/format'

export function FloodProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const toast = useToast()
  const [remaining, setRemaining] = useState<number | null>(null)
  const [startedAt, setStartedAt] = useState<number | null>(null)
  const running = useRef(false)

  const start = useCallback(async () => {
    if (running.current) return
    running.current = true
    setRemaining(FLOOD_SIZE)
    toast(`Submitting ${formatNumber(FLOOD_SIZE)} ${FLOOD_JOB.type} jobs in batches of ${formatNumber(FLOOD_BATCH)}.`)

    let sent = 0
    try {
      while (sent < FLOOD_SIZE) {
        await submitBatch(Array.from({ length: FLOOD_BATCH }, () => FLOOD_JOB))
        if (sent === 0) setStartedAt(Date.now())
        sent += FLOOD_BATCH
        setRemaining(FLOOD_SIZE - sent)
      }
    } catch (err) {
      toast(sent > 0 ? `Stopped after ${formatNumber(sent)} jobs. ${describeError(err)}` : describeError(err))
    } finally {
      running.current = false
      setRemaining(null)
      void queryClient.invalidateQueries({ queryKey: queryKeys.jobs })
    }
  }, [queryClient, toast])

  const flood = useMemo(() => ({ start: () => void start(), remaining, startedAt }), [start, remaining, startedAt])

  return <FloodContext value={flood}>{children}</FloodContext>
}

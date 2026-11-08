import { createContext, useContext } from 'react'

export interface Flood {
  start: () => void
  // Jobs still to submit while a flood is running, otherwise null.
  remaining: number | null
  // When the last flood started, so the queue chart can mark it.
  startedAt: number | null
}

export const FloodContext = createContext<Flood | null>(null)

export function useFlood(): Flood {
  const flood = useContext(FloodContext)
  if (!flood) throw new Error('useFlood must be used inside <FloodProvider>')
  return flood
}

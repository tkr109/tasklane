import { useSyncExternalStore } from 'react'

const TICK_MS = 500

const listeners = new Set<() => void>()
let now = Date.now()
let timer: number | undefined

function subscribe(listener: () => void) {
  listeners.add(listener)
  timer ??= window.setInterval(() => {
    now = Date.now()
    listeners.forEach((l) => l())
  }, TICK_MS)
  return () => {
    listeners.delete(listener)
    if (listeners.size === 0) {
      window.clearInterval(timer)
      timer = undefined
    }
  }
}

// A shared clock so relative times ("4s ago") stay fresh without each row running its own timer.
export function useNow() {
  return useSyncExternalStore(subscribe, () => now)
}

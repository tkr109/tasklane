import { useCallback, useState, type ReactNode } from 'react'
import { ToastContext } from '../hooks/useToast'

interface Toast {
  id: number
  message: string
}

const SHOW_MS = 4200

let nextId = 1

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])

  const show = useCallback((message: string) => {
    const id = nextId++
    setToasts((list) => [...list, { id, message }])
    window.setTimeout(() => setToasts((list) => list.filter((t) => t.id !== id)), SHOW_MS)
  }, [])

  return (
    <ToastContext value={show}>
      {children}
      <div className="toasts" aria-live="polite">
        {toasts.map((t) => (
          <div key={t.id} className="toast">
            {t.message}
          </div>
        ))}
      </div>
    </ToastContext>
  )
}

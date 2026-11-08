import { createContext, useContext } from 'react'

export type ShowToast = (message: string) => void

export const ToastContext = createContext<ShowToast | null>(null)

export function useToast(): ShowToast {
  const toast = useContext(ToastContext)
  if (!toast) throw new Error('useToast must be used inside <ToastProvider>')
  return toast
}

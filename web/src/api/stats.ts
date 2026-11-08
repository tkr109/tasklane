import { admin } from './client'
import type { Stats, WorkersResponse } from './types'

export const getStats = () => admin.get<Stats>('/api/stats')

export const getWorkers = () => admin.get<WorkersResponse>('/api/workers')

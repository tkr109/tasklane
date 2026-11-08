import { get } from './client'
import type { Stats, WorkersResponse } from './types'

export const getStats = () => get<Stats>('/api/stats')

export const getWorkers = () => get<WorkersResponse>('/api/workers')

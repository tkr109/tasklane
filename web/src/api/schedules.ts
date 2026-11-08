import { api } from './client'
import type { Schedule, ScheduleSpec } from './types'

export const getSchedules = () => api.get<{ schedules: Schedule[] }>('/api/schedules')

export const createSchedule = (spec: ScheduleSpec) => api.post<Schedule>('/api/schedules', spec)

export const deleteSchedule = (id: number) => api.del(`/api/schedules/${id}`)

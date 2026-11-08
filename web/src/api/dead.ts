import { admin } from './client'
import type { DeadGroupsResponse, Job } from './types'

export const getDeadGroups = () => admin.get<DeadGroupsResponse>('/api/dead-groups')

export const getDeadGroupJobs = (groupId: string, limit = 5) =>
  admin.get<{ jobs: Job[] }>(`/api/dead-groups/${encodeURIComponent(groupId)}/jobs?limit=${limit}`)

export const retryDeadGroup = (groupId: string) =>
  admin.post<{ retried: number }>(`/api/dead-groups/${encodeURIComponent(groupId)}/retry`)

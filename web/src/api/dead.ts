import { get, post } from './client'
import type { DeadGroupsResponse, Job } from './types'

export const getDeadGroups = () => get<DeadGroupsResponse>('/api/dead-groups')

export const getDeadGroupJobs = (groupId: string, limit = 5) =>
  get<{ jobs: Job[] }>(`/api/dead-groups/${encodeURIComponent(groupId)}/jobs?limit=${limit}`)

export const retryDeadGroup = (groupId: string) =>
  post<{ retried: number }>(`/api/dead-groups/${encodeURIComponent(groupId)}/retry`)

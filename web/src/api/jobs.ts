import { get, post } from './client'
import type { Job, JobDetail, JobFilters, JobPage, JobSpec } from './types'

export function listJobs(filters: JobFilters, cursor?: string) {
  const params = new URLSearchParams()
  if (filters.status) params.set('status', filters.status)
  if (filters.type) params.set('type', filters.type)
  if (cursor) params.set('cursor', cursor)
  const qs = params.toString()
  return get<JobPage>(qs ? `/api/jobs?${qs}` : '/api/jobs')
}

export const getJob = (id: number) => get<JobDetail>(`/api/jobs/${id}`)

export const cancelJob = (id: number) => post<Job>(`/api/jobs/${id}/cancel`)

export const retryJob = (id: number) => post<Job>(`/api/jobs/${id}/retry`)

export const submitBatch = (jobs: JobSpec[]) => post<{ jobs: Job[] }>('/api/jobs/batch', { jobs })

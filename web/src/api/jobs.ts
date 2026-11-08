import { admin, api, ApiError } from './client'
import type { Job, JobDetail, JobFilters, JobPage, JobSpec } from './types'

export function listJobs(filters: JobFilters, cursor?: string) {
  const params = new URLSearchParams()
  if (filters.status) params.set('status', filters.status)
  if (filters.type) params.set('type', filters.type)
  if (cursor) params.set('cursor', cursor)
  const qs = params.toString()
  return api.get<JobPage>(qs ? `/api/jobs?${qs}` : '/api/jobs')
}

// Workers and dead groups link to every team's jobs, so the job page reads through
// the admin route, and with the team's key only if the admin token is refused.
export async function getJob(id: number) {
  try {
    return await admin.get<JobDetail>(`/api/admin/jobs/${id}`)
  } catch (err) {
    if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
      return api.get<JobDetail>(`/api/jobs/${id}`)
    }
    throw err
  }
}

export const cancelJob = (id: number) => api.post<Job>(`/api/jobs/${id}/cancel`)

export const retryJob = (id: number) => api.post<Job>(`/api/jobs/${id}/retry`)

export const submitBatch = (jobs: JobSpec[]) => api.post<{ jobs: Job[] }>('/api/jobs/batch', { jobs })

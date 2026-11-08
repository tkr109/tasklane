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

// Workers and dead groups link to every team's jobs, so the job page goes through
// the admin routes, and through the team's key only if the admin token is refused.
async function asAdminOrTeam<T>(asAdmin: () => Promise<T>, asTeam: () => Promise<T>) {
  try {
    return await asAdmin()
  } catch (err) {
    if (err instanceof ApiError && (err.status === 401 || err.status === 403)) return asTeam()
    throw err
  }
}

export const getJob = (id: number) =>
  asAdminOrTeam(
    () => admin.get<JobDetail>(`/api/admin/jobs/${id}`),
    () => api.get<JobDetail>(`/api/jobs/${id}`),
  )

export const cancelJob = (id: number) => api.post<Job>(`/api/jobs/${id}/cancel`)

export const retryJob = (id: number) => api.post<Job>(`/api/jobs/${id}/retry`)

export const cancelAnyJob = (id: number) =>
  asAdminOrTeam(() => admin.post<Job>(`/api/admin/jobs/${id}/cancel`), () => cancelJob(id))

export const retryAnyJob = (id: number) =>
  asAdminOrTeam(() => admin.post<Job>(`/api/admin/jobs/${id}/retry`), () => retryJob(id))

export const submitBatch = (jobs: JobSpec[]) => api.post<{ jobs: Job[] }>('/api/jobs/batch', { jobs })

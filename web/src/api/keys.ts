import type { JobFilters } from './types'

export const queryKeys = {
  jobs: ['jobs'] as const,
  jobList: (filters: JobFilters) => ['jobs', 'list', filters] as const,
  job: (id: number) => ['jobs', 'detail', id] as const,
  stats: ['stats'] as const,
  workers: ['workers'] as const,
  teams: ['teams'] as const,
  deadGroups: ['dead-groups'] as const,
  deadGroupJobs: (id: string) => ['dead-groups', id, 'jobs'] as const,
}

import { queryOptions } from '@tanstack/react-query'
import { getDeadGroupJobs, getDeadGroups } from './dead'
import { getJob } from './jobs'
import { queryKeys } from './keys'
import { getStats, getWorkers } from './stats'
import { getTeams } from './teams'

export const statsQuery = queryOptions({ queryKey: queryKeys.stats, queryFn: getStats })

export const workersQuery = queryOptions({ queryKey: queryKeys.workers, queryFn: getWorkers })

export const teamsQuery = queryOptions({ queryKey: queryKeys.teams, queryFn: getTeams })

export const deadGroupsQuery = queryOptions({ queryKey: queryKeys.deadGroups, queryFn: getDeadGroups })

export const deadGroupJobsQuery = (groupId: string) =>
  queryOptions({ queryKey: queryKeys.deadGroupJobs(groupId), queryFn: () => getDeadGroupJobs(groupId) })

export const jobQuery = (id: number) => queryOptions({ queryKey: queryKeys.job(id), queryFn: () => getJob(id) })

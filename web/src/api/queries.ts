import { queryOptions } from '@tanstack/react-query'
import { getDeadGroupJobs, getDeadGroups } from './dead'
import { getJob } from './jobs'
import { queryKeys } from './keys'
import { getSchedules } from './schedules'
import { getStats, getWorkers } from './stats'
import { getKeys, getPlans, getTeams } from './teams'

export const statsQuery = queryOptions({ queryKey: queryKeys.stats, queryFn: getStats })

export const workersQuery = queryOptions({ queryKey: queryKeys.workers, queryFn: getWorkers })

export const plansQuery = queryOptions({ queryKey: queryKeys.plans, queryFn: getPlans, staleTime: 60_000 })

export const teamsQuery = queryOptions({ queryKey: queryKeys.teams, queryFn: getTeams })

export const teamKeysQuery = (teamId: number) =>
  queryOptions({ queryKey: queryKeys.teamKeys(teamId), queryFn: () => getKeys(teamId) })

export const deadGroupsQuery = queryOptions({ queryKey: queryKeys.deadGroups, queryFn: getDeadGroups })

export const deadGroupJobsQuery = (groupId: string) =>
  queryOptions({ queryKey: queryKeys.deadGroupJobs(groupId), queryFn: () => getDeadGroupJobs(groupId) })

export const schedulesQuery = queryOptions({ queryKey: queryKeys.schedules, queryFn: getSchedules })

export const jobQuery = (id: number) => queryOptions({ queryKey: queryKeys.job(id), queryFn: () => getJob(id) })

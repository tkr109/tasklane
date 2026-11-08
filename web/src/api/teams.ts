import { admin } from './client'
import type { ApiKey, NewApiKey, Plan, PlanName, Team, TeamSummary } from './types'

export const getPlans = () => admin.get<{ plans: Plan[] }>('/api/plans')

export const getTeams = () => admin.get<{ teams: TeamSummary[] }>('/api/teams')

export const switchPlan = (teamId: number, plan: PlanName) => admin.put<Team>(`/api/teams/${teamId}/plan`, { plan })

export const getKeys = (teamId: number) => admin.get<{ keys: ApiKey[] }>(`/api/teams/${teamId}/keys`)

export const createKey = (teamId: number) => admin.post<NewApiKey>(`/api/teams/${teamId}/keys`)

export const revokeKey = (teamId: number, keyId: number) =>
  admin.post<ApiKey>(`/api/teams/${teamId}/keys/${keyId}/revoke`)

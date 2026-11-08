import { get, patch, post } from './client'
import type { ApiKey, CreatedKey, PlanName, Team, TeamsResponse } from './types'

export const getTeams = () => get<TeamsResponse>('/api/teams')

export const switchPlan = (teamId: number, plan: PlanName) => patch<Team>(`/api/teams/${teamId}`, { plan })

export const createKey = (teamId: number, name: string) =>
  post<CreatedKey>(`/api/teams/${teamId}/keys`, { name })

export const revokeKey = (teamId: number, keyId: number) =>
  post<ApiKey>(`/api/teams/${teamId}/keys/${keyId}/revoke`)

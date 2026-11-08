import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { describeError } from '../../api/client'
import { queryKeys } from '../../api/keys'
import { teamsQuery } from '../../api/queries'
import { createKey, revokeKey, switchPlan } from '../../api/teams'
import type { ApiKey, PlanName, Team, TeamsResponse } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { QueryNotice } from '../../components/QueryNotice'
import { useNow } from '../../hooks/useNow'
import { useToast } from '../../hooks/useToast'
import { capitalize, formatNumber } from '../../lib/format'
import { ApiKeysCard } from './ApiKeysCard'
import { PlanCards } from './PlanCards'
import { TeamsTable } from './TeamsTable'

export function TeamsPage() {
  const queryClient = useQueryClient()
  const toast = useToast()
  const now = useNow()
  const { data, error } = useQuery(teamsQuery)
  const [pickedId, setPickedId] = useState<number>()
  const [newKey, setNewKey] = useState<{ teamId: number; secret: string } | null>(null)

  const updateTeam = (teamId: number, update: (team: Team) => Team) =>
    queryClient.setQueryData<TeamsResponse>(queryKeys.teams, (prev) =>
      prev && { ...prev, teams: prev.teams.map((t) => (t.id === teamId ? update(t) : t)) },
    )

  const planMutation = useMutation({
    mutationFn: ({ team, plan }: { team: Team; plan: PlanName }) => switchPlan(team.id, plan),
    onSuccess: (updated, { team }) => {
      updateTeam(team.id, () => updated)
      const plan = data?.plans.find((p) => p.name === updated.plan)
      const limits = plan
        ? `: weight ${plan.share_weight}, worker cap ${plan.max_workers}, ${formatNumber(plan.jobs_per_minute)} jobs per minute`
        : ''
      toast(`${team.name} moved from ${capitalize(team.plan)} to ${capitalize(updated.plan)}${limits}.`)
    },
    onError: (err) => toast(describeError(err)),
  })

  const createKeyMutation = useMutation({
    mutationFn: (teamId: number) => createKey(teamId, 'new key'),
    onSuccess: ({ key, secret }, teamId) => {
      updateTeam(teamId, (t) => ({ ...t, keys: [key, ...t.keys] }))
      setNewKey({ teamId, secret })
    },
    onError: (err) => toast(describeError(err)),
  })

  const revokeMutation = useMutation({
    mutationFn: ({ teamId, key }: { teamId: number; key: ApiKey }) => revokeKey(teamId, key.id),
    onSuccess: (revoked, { teamId }) => {
      updateTeam(teamId, (t) => ({ ...t, keys: t.keys.map((k) => (k.id === revoked.id ? revoked : k)) }))
      const team = data?.teams.find((t) => t.id === teamId)
      toast(`Revoked ${revoked.prefix}… for ${team?.name ?? 'the team'}.`)
    },
    onError: (err) => toast(describeError(err)),
  })

  const header = (
    <PageHeader
      title="Teams"
      sub="A team's plan sets its fair-share weight, worker cap and submit rate. Switching plan is an admin action and takes effect at once; there is no billing."
    />
  )
  if (!data) {
    return (
      <>
        {header}
        <QueryNotice error={error} what="teams" />
      </>
    )
  }

  const picked = data.teams.find((t) => t.id === pickedId) ?? data.teams[0]

  const copySecret = (secret: string) => {
    navigator.clipboard.writeText(secret).then(
      () => toast('Key copied.'),
      () => toast('Select the key and copy it by hand.'),
    )
  }

  return (
    <>
      {header}
      <PlanCards plans={data.plans} />
      <div className="card teams-card">
        <div className="card-h">
          <h2>All teams</h2>
          <span className="sub">Click a team to manage its API keys</span>
        </div>
        <TeamsTable
          teams={data.teams}
          plans={data.plans}
          pickedId={picked?.id}
          onPick={setPickedId}
          onSwitchPlan={(team, plan) => planMutation.mutate({ team, plan })}
        />
      </div>
      {picked && (
        <ApiKeysCard
          team={picked}
          newSecret={newKey?.teamId === picked.id ? newKey.secret : null}
          now={now}
          creating={createKeyMutation.isPending}
          onCreate={() => createKeyMutation.mutate(picked.id)}
          onCopySecret={() => newKey && copySecret(newKey.secret)}
          onDismissSecret={() => setNewKey(null)}
          onRevoke={(key) => revokeMutation.mutate({ teamId: picked.id, key })}
        />
      )}
    </>
  )
}

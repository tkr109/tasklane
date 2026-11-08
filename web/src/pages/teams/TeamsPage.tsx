import { skipToken, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { describeError } from '../../api/client'
import { queryKeys } from '../../api/keys'
import { plansQuery, teamsQuery } from '../../api/queries'
import { createKey, revokeKey, switchPlan } from '../../api/teams'
import type { ApiKey, PlanName, TeamRate, TeamSummary } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { QueryNotice } from '../../components/QueryNotice'
import { useNow } from '../../hooks/useNow'
import { useToast } from '../../hooks/useToast'
import { capitalize, formatNumber } from '../../lib/format'
import { ApiKeysCard } from './ApiKeysCard'
import { PlanCards } from './PlanCards'
import { TeamsTable } from './TeamsTable'

type KeysData = { keys: ApiKey[] }

export function TeamsPage() {
  const queryClient = useQueryClient()
  const toast = useToast()
  const now = useNow()
  const { data: teamsData, error: teamsError } = useQuery(teamsQuery)
  const { data: plansData, error: plansError } = useQuery(plansQuery)
  // Filled only by the event stream.
  const { data: rates } = useQuery<Record<number, TeamRate>>({ queryKey: queryKeys.teamRates, queryFn: skipToken })
  const [pickedId, setPickedId] = useState<number>()
  const [newKey, setNewKey] = useState<{ teamId: number; key: string } | null>(null)

  const updateKeys = (teamId: number, update: (keys: ApiKey[]) => ApiKey[]) =>
    queryClient.setQueryData<KeysData>(queryKeys.teamKeys(teamId), (prev) => prev && { keys: update(prev.keys) })

  const planMutation = useMutation({
    mutationFn: ({ team, plan }: { team: TeamSummary; plan: PlanName }) => switchPlan(team.id, plan),
    onSuccess: (updated, { team }) => {
      queryClient.setQueryData<{ teams: TeamSummary[] }>(
        queryKeys.teams,
        (prev) => prev && { teams: prev.teams.map((t) => (t.id === team.id ? { ...t, plan: updated.plan } : t)) },
      )
      const p = updated.plan
      toast(
        `${team.name} moved from ${capitalize(team.plan.name)} to ${capitalize(p.name)}: weight ${p.share_weight}, worker cap ${p.max_workers}, ${formatNumber(p.jobs_per_minute)} jobs per minute.`,
      )
    },
    onError: (err) => toast(describeError(err)),
  })

  const createKeyMutation = useMutation({
    mutationFn: (teamId: number) => createKey(teamId),
    onSuccess: ({ key, ...stored }, teamId) => {
      updateKeys(teamId, (keys) => [...keys, stored])
      setNewKey({ teamId, key })
    },
    onError: (err) => toast(describeError(err)),
  })

  const revokeMutation = useMutation({
    mutationFn: (key: ApiKey) => revokeKey(key.team_id, key.id),
    onSuccess: (revoked) => {
      updateKeys(revoked.team_id, (keys) => keys.map((k) => (k.id === revoked.id ? revoked : k)))
      const team = teamsData?.teams.find((t) => t.id === revoked.team_id)
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
  if (!teamsData || !plansData) {
    return (
      <>
        {header}
        <QueryNotice error={teamsError ?? plansError} what="teams" />
      </>
    )
  }

  const picked = teamsData.teams.find((t) => t.id === pickedId) ?? teamsData.teams[0]

  const copyNewKey = (key: string) => {
    navigator.clipboard.writeText(key).then(
      () => toast('Key copied.'),
      () => toast('Select the key and copy it by hand.'),
    )
  }

  return (
    <>
      {header}
      <PlanCards plans={plansData.plans} />
      <div className="card teams-card">
        <div className="card-h">
          <h2>All teams</h2>
          <span className="sub">Click a team to manage its API keys</span>
        </div>
        <TeamsTable
          teams={teamsData.teams}
          plans={plansData.plans}
          rates={rates}
          pickedId={picked?.id}
          now={now}
          onPick={setPickedId}
          onSwitchPlan={(team, plan) => planMutation.mutate({ team, plan })}
        />
      </div>
      {picked && (
        <ApiKeysCard
          team={picked}
          newKey={newKey?.teamId === picked.id ? newKey.key : null}
          now={now}
          creating={createKeyMutation.isPending}
          onCreate={() => createKeyMutation.mutate(picked.id)}
          onCopyNewKey={() => newKey && copyNewKey(newKey.key)}
          onDismissNewKey={() => setNewKey(null)}
          onRevoke={(key) => revokeMutation.mutate(key)}
        />
      )}
    </>
  )
}

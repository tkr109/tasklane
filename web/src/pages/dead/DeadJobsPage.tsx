import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { describeError } from '../../api/client'
import { retryDeadGroup } from '../../api/dead'
import { queryKeys } from '../../api/keys'
import { deadGroupsQuery, teamsQuery } from '../../api/queries'
import type { DeadGroup, DeadGroupsResponse } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { QueryNotice } from '../../components/QueryNotice'
import { useNow } from '../../hooks/useNow'
import { useToast } from '../../hooks/useToast'
import { plural } from '../../lib/format'
import { DeadGroupCard } from './DeadGroupCard'

// Long enough for the card's fade-out to finish before it leaves the list.
const LEAVE_MS = 330

export function DeadJobsPage() {
  const queryClient = useQueryClient()
  const toast = useToast()
  const now = useNow()
  const { data, error } = useQuery(deadGroupsQuery)
  const { data: teamsData } = useQuery(teamsQuery)
  const [leaving, setLeaving] = useState<string | null>(null)

  const retry = useMutation({
    mutationFn: (group: DeadGroup) => retryDeadGroup(group.id),
    onMutate: (group) => setLeaving(group.id),
    onSuccess: ({ retried }, group) => {
      window.setTimeout(() => {
        queryClient.setQueryData<DeadGroupsResponse>(
          queryKeys.deadGroups,
          (prev) => prev && { groups: prev.groups.filter((g) => g.id !== group.id), total: prev.total - group.count },
        )
        setLeaving(null)
        void queryClient.invalidateQueries({ queryKey: queryKeys.stats })
      }, LEAVE_MS)
      toast(
        retried === 0
          ? 'Nothing left to retry: the group was already emptied, probably by another retry.'
          : `${plural(retried, 'job')} from "${group.pattern}" ${retried === 1 ? 'is' : 'are'} back in the queue. Workers scale up if the queue needs it.`,
      )
    },
    onError: (err) => {
      setLeaving(null)
      toast(describeError(err))
    },
  })

  const teamName = (id: number) => teamsData?.teams.find((t) => t.id === id)?.name ?? `team ${id}`

  if (!data) {
    return (
      <>
        <PageHeader title="Dead jobs" />
        <QueryNotice error={error} what="dead jobs" />
      </>
    )
  }

  return (
    <>
      <PageHeader
        title="Dead jobs"
        sub={`${plural(data.total, 'job')} in ${plural(data.groups.length, 'group')}. A job lands here when its last attempt fails. Retrying a group puts its jobs back in the queue with attempts reset.`}
      />
      {data.groups.length === 0 ? (
        <div className="card empty">No dead jobs. Every job either finished or is still being retried.</div>
      ) : (
        <div className="groups">
          {data.groups.map((group) => (
            <DeadGroupCard
              key={group.id}
              group={group}
              now={now}
              leaving={leaving === group.id}
              teamName={teamName}
              onRetry={() => retry.mutate(group)}
            />
          ))}
        </div>
      )}
    </>
  )
}

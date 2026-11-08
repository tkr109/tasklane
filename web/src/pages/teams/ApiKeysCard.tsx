import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { teamKeysQuery } from '../../api/queries'
import type { ApiKey, TeamSummary } from '../../api/types'
import { PlanBadge } from '../../components/PlanBadge'
import { QueryNotice } from '../../components/QueryNotice'
import { formatAgo } from '../../lib/format'

interface ApiKeysCardProps {
  team: TeamSummary
  newKey: string | null
  now: number
  creating: boolean
  onCreate: () => void
  onCopyNewKey: () => void
  onDismissNewKey: () => void
  onRevoke: (key: ApiKey) => void
}

export function ApiKeysCard(props: ApiKeysCardProps) {
  const { team, newKey, now, creating, onCreate, onCopyNewKey, onDismissNewKey, onRevoke } = props
  const { data, error } = useQuery(teamKeysQuery(team.id))
  const [confirming, setConfirming] = useState<number | null>(null)
  const keys = data ? [...data.keys].reverse() : []

  return (
    <div className="card">
      <div className="card-h">
        <h2>API keys for {team.name}</h2>
        <PlanBadge plan={team.plan.name} />
        <span className="grow" />
        <button className="btn" onClick={onCreate} disabled={creating}>
          Create key
        </button>
      </div>
      {newKey && (
        <div className="new-key">
          <b>New key for {team.name}.</b> Copy it now; only its prefix is stored, so it can't be shown again.
          <code>{newKey}</code>
          <button className="btn sm" onClick={onCopyNewKey}>
            Copy
          </button>
          <button className="btn sm ghost" onClick={onDismissNewKey}>
            Done
          </button>
        </div>
      )}
      {!data ? (
        <QueryNotice error={error} what="keys" className="card-note" />
      ) : keys.length === 0 ? (
        <p className="card-note">This team has no API keys yet.</p>
      ) : (
        <ul className="keys">
          {keys.map((key) => (
            <li key={key.id} className={key.revoked_at ? 'revoked' : undefined}>
              <span className="mono prefix">{key.prefix}…</span>
              <span className="meta">
                created {formatAgo(key.created_at, now)}
                {key.revoked_at && ` · revoked ${formatAgo(key.revoked_at, now)}`}
              </span>
              <span className="grow" />
              {key.revoked_at ? (
                <span className="pill s-cancelled nodot">revoked</span>
              ) : confirming === key.id ? (
                <span className="confirm">
                  Apps using this key will get 401.
                  <button
                    className="btn sm danger solid"
                    onClick={() => {
                      setConfirming(null)
                      onRevoke(key)
                    }}
                  >
                    Revoke
                  </button>
                  <button className="btn sm ghost" onClick={() => setConfirming(null)}>
                    Keep
                  </button>
                </span>
              ) : (
                <button className="btn sm danger" onClick={() => setConfirming(key.id)}>
                  Revoke
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

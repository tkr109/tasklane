import { useState } from 'react'
import type { ApiKey, Team } from '../../api/types'
import { PlanBadge } from '../../components/PlanBadge'
import { formatAgo } from '../../lib/format'

interface ApiKeysCardProps {
  team: Team
  newSecret: string | null
  now: number
  creating: boolean
  onCreate: () => void
  onCopySecret: () => void
  onDismissSecret: () => void
  onRevoke: (key: ApiKey) => void
}

function keyMeta(key: ApiKey, now: number) {
  const created = `created ${formatAgo(key.created_at, now)}`
  if (key.revoked_at) return `${created} · revoked ${formatAgo(key.revoked_at, now)}`
  if (!key.last_used_at) return `${created} · never used`
  return `${created} · last used ${formatAgo(key.last_used_at, now)}`
}

export function ApiKeysCard({ team, newSecret, now, creating, onCreate, onCopySecret, onDismissSecret, onRevoke }: ApiKeysCardProps) {
  const [confirming, setConfirming] = useState<number | null>(null)

  return (
    <div className="card">
      <div className="card-h">
        <h2>API keys for {team.name}</h2>
        <PlanBadge plan={team.plan} />
        <span className="grow" />
        <button className="btn" onClick={onCreate} disabled={creating}>
          Create key
        </button>
      </div>
      {newSecret && (
        <div className="new-key">
          <b>New key for {team.name}.</b> Copy it now; only its prefix is stored, so it can't be shown again.
          <code>{newSecret}</code>
          <button className="btn sm" onClick={onCopySecret}>
            Copy
          </button>
          <button className="btn sm ghost" onClick={onDismissSecret}>
            Done
          </button>
        </div>
      )}
      <ul className="keys">
        {team.keys.map((key) => (
          <li key={key.id} className={key.revoked_at ? 'revoked' : undefined}>
            <span className="mono prefix">{key.prefix}…</span>
            <span>{key.name}</span>
            <span className="meta">{keyMeta(key, now)}</span>
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
      {team.keys.length === 0 && <p className="card-note">This team has no API keys yet.</p>}
    </div>
  )
}

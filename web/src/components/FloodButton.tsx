import { useFlood } from '../hooks/useFlood'
import { formatNumber } from '../lib/format'
import { DownloadIcon } from './icons'

export function FloodButton({ primary = false }: { primary?: boolean }) {
  const { start, remaining } = useFlood()
  const busy = remaining !== null

  return (
    <button className={primary ? 'btn primary' : 'btn'} onClick={start} disabled={busy}>
      {primary && <DownloadIcon />}
      <span>{busy ? `Submitting… ${formatNumber(remaining)} left` : 'Flood 10k jobs'}</span>
    </button>
  )
}

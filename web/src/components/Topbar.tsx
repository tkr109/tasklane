import type { LiveStatus } from '../hooks/useLiveEvents'
import { toggleTheme } from '../lib/theme'
import { MoonIcon } from './icons'

const LIVE_TEXT: Record<LiveStatus, { className: string; label: string }> = {
  live: { className: 'on', label: 'Live, via event stream' },
  connecting: { className: 'connecting', label: 'Connecting to event stream' },
  offline: { className: 'off', label: 'Event stream unavailable' },
}

export function Topbar({ title, live }: { title: string; live: LiveStatus }) {
  const { className, label } = LIVE_TEXT[live]
  return (
    <header className="top">
      <span className="crumb">{title}</span>
      <span className="grow" />
      <span className={`live ${className}`}>
        <span className="dot" />
        <span className="label">{label}</span>
      </span>
      <button
        className="btn icon ghost"
        onClick={toggleTheme}
        title="Switch light or dark"
        aria-label="Switch light or dark"
      >
        <MoonIcon />
      </button>
    </header>
  )
}

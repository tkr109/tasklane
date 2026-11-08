import { toggleTheme } from '../lib/theme'
import { MoonIcon } from './icons'

export function Topbar({ title }: { title: string }) {
  return (
    <header className="top">
      <span className="crumb">{title}</span>
      <span className="grow" />
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

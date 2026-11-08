import type { ReactNode } from 'react'

const TOKEN = /("(?:\\.|[^"\\])*")(\s*:)?|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g

export function JsonView({ value }: { value: unknown }) {
  const text = JSON.stringify(value, null, 2) ?? 'null'
  const parts: ReactNode[] = []
  let last = 0

  for (const match of text.matchAll(TOKEN)) {
    const [whole, str, colon, literal] = match
    parts.push(text.slice(last, match.index))
    const kind = str ? (colon ? 'k' : 's') : literal ? 'l' : 'n'
    parts.push(
      <span key={match.index} className={kind}>
        {str ?? whole}
      </span>,
    )
    if (colon) parts.push(colon)
    last = match.index + whole.length
  }
  parts.push(text.slice(last))

  return <pre className="json">{parts}</pre>
}

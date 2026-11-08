export function Meter({ percent }: { percent: number }) {
  const level = percent >= 99.5 ? 'full' : percent > 80 ? 'warn' : undefined
  return (
    <div className="meter">
      <i className={level} style={{ width: `${Math.min(100, percent)}%` }} />
    </div>
  )
}

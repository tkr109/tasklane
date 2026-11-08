const WIDTH = 96
const HEIGHT = 26

export function Sparkline({ values, label }: { values: number[]; label: string }) {
  const max = Math.max(...values, 1)
  const step = WIDTH / Math.max(1, values.length - 1)
  const line = values
    .map((v, i) => `${i ? 'L' : 'M'}${(i * step).toFixed(1)} ${(HEIGHT - 2 - (v / max) * (HEIGHT - 4)).toFixed(1)}`)
    .join('')

  return (
    <svg className="spark" width={WIDTH} height={HEIGHT} viewBox={`0 0 ${WIDTH} ${HEIGHT}`} role="img" aria-label={label}>
      <path className="area" d={`${line}L${WIDTH} ${HEIGHT}L0 ${HEIGHT}Z`} />
      <path d={line} />
    </svg>
  )
}

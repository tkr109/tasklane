interface BulkBarProps {
  count: number
  onRetry: () => void
  onCancel: () => void
  onClear: () => void
}

export function BulkBar({ count, onRetry, onCancel, onClear }: BulkBarProps) {
  return (
    <div className="bulk">
      <b>{count.toLocaleString('en-US')} selected</b>
      <span className="grow" />
      <button className="btn sm" onClick={onRetry}>
        Retry
      </button>
      <button className="btn sm danger" onClick={onCancel}>
        Cancel
      </button>
      <button className="btn sm ghost" onClick={onClear}>
        Clear selection
      </button>
    </div>
  )
}

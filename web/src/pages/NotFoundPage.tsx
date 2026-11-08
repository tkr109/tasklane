import { Link } from 'react-router'

export function NotFoundPage() {
  return (
    <div className="card empty">
      Nothing lives at this address. <Link to="/">Go to the overview</Link>.
    </div>
  )
}

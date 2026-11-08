import { useQuery } from '@tanstack/react-query'
import type { ComponentType } from 'react'
import { NavLink } from 'react-router'
import { statsQuery } from '../api/queries'
import { formatShort } from '../lib/format'
import { DeadIcon, JobsIcon, Logo, OverviewIcon, SchedulesIcon, TeamsIcon, WorkersIcon } from './icons'

interface NavItemProps {
  to: string
  label: string
  Icon: ComponentType
  count?: string
  bad?: boolean
}

function NavItem({ to, label, Icon, count, bad }: NavItemProps) {
  return (
    <NavLink to={to} end={to === '/'}>
      <Icon />
      {label}
      {count !== undefined && <span className={bad ? 'count bad' : 'count'}>{count}</span>}
    </NavLink>
  )
}

export function Sidebar() {
  const { data: stats } = useQuery(statsQuery)
  const queued = stats?.counts.queued ?? 0

  return (
    <aside className="side">
      <div className="brand">
        <Logo />
        <span>Tasklane</span>
      </div>
      <nav className="nav">
        <NavItem to="/" label="Overview" Icon={OverviewIcon} />
        <NavItem to="/jobs" label="Jobs" Icon={JobsIcon} count={queued >= 1 ? formatShort(queued) : undefined} />
        <NavItem to="/schedules" label="Schedules" Icon={SchedulesIcon} />
        <NavItem to="/workers" label="Workers" Icon={WorkersIcon} count={stats && String(stats.workers.running)} />
        <NavItem to="/teams" label="Teams" Icon={TeamsIcon} />
        <NavItem to="/dead" label="Dead jobs" Icon={DeadIcon} count={stats && formatShort(stats.counts.dead)} bad />
      </nav>
      <div className="side-foot">
        <span>Environment: {import.meta.env.VITE_MOCK === '1' ? 'mock data' : 'local'}</span>
        <span className="mono">tasklane v{__APP_VERSION__}</span>
      </div>
    </aside>
  )
}

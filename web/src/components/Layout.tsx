import { useEffect } from 'react'
import { Outlet, useLocation, useMatches } from 'react-router'
import { Sidebar } from './Sidebar'
import { Topbar } from './Topbar'

export interface RouteHandle {
  title: string
}

export function Layout() {
  const matches = useMatches()
  const { pathname } = useLocation()
  const handle = matches.at(-1)?.handle as RouteHandle | undefined
  const title = handle?.title ?? 'Not found'

  useEffect(() => {
    window.scrollTo(0, 0)
  }, [pathname])

  useEffect(() => {
    document.title = `${title} · Tasklane`
  }, [title])

  return (
    <div className="app">
      <Sidebar />
      <div className="main">
        <Topbar title={title} />
        <main className="content">
          <Outlet />
        </main>
      </div>
    </div>
  )
}

import { createBrowserRouter } from 'react-router'
import { Layout, type RouteHandle } from './components/Layout'
import { NotFoundPage } from './pages/NotFoundPage'

const handle = (title: string): RouteHandle => ({ title })

// Pages load on first visit, so the charts only come down with the overview.
export const router = createBrowserRouter([
  {
    element: <Layout />,
    // Shown for the moment the first page's code is loading.
    hydrateFallbackElement: <div className="app" />,
    children: [
      {
        index: true,
        handle: handle('Overview'),
        lazy: async () => ({ Component: (await import('./pages/overview/OverviewPage')).OverviewPage }),
      },
      {
        path: 'jobs',
        handle: handle('Jobs'),
        lazy: async () => ({ Component: (await import('./pages/jobs/JobsPage')).JobsPage }),
      },
      {
        path: 'jobs/:id',
        handle: handle('Jobs / Job detail'),
        lazy: async () => ({ Component: (await import('./pages/job/JobDetailPage')).JobDetailPage }),
      },
      {
        path: 'workers',
        handle: handle('Workers'),
        lazy: async () => ({ Component: (await import('./pages/workers/WorkersPage')).WorkersPage }),
      },
      {
        path: 'teams',
        handle: handle('Teams'),
        lazy: async () => ({ Component: (await import('./pages/teams/TeamsPage')).TeamsPage }),
      },
      {
        path: 'dead',
        handle: handle('Dead jobs'),
        lazy: async () => ({ Component: (await import('./pages/dead/DeadJobsPage')).DeadJobsPage }),
      },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
])

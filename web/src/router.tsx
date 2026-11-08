import { createBrowserRouter } from 'react-router'
import { Layout, type RouteHandle } from './components/Layout'
import { JobDetailPage } from './pages/job/JobDetailPage'
import { JobsPage } from './pages/jobs/JobsPage'
import { NotFoundPage } from './pages/NotFoundPage'
import { OverviewPage } from './pages/overview/OverviewPage'
import { WorkersPage } from './pages/workers/WorkersPage'

const handle = (title: string): RouteHandle => ({ title })

export const router = createBrowserRouter([
  {
    element: <Layout />,
    children: [
      { index: true, element: <OverviewPage />, handle: handle('Overview') },
      { path: 'jobs', element: <JobsPage />, handle: handle('Jobs') },
      { path: 'jobs/:id', element: <JobDetailPage />, handle: handle('Jobs / Job detail') },
      { path: 'workers', element: <WorkersPage />, handle: handle('Workers') },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
])

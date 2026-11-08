import { createBrowserRouter } from 'react-router'
import { Layout, type RouteHandle } from './components/Layout'
import { NotFoundPage } from './pages/NotFoundPage'
import { OverviewPage } from './pages/OverviewPage'

const handle = (title: string): RouteHandle => ({ title })

export const router = createBrowserRouter([
  {
    element: <Layout />,
    children: [
      { index: true, element: <OverviewPage />, handle: handle('Overview') },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
])

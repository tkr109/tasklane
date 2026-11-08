import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from 'react-router'
import { ApiError } from './api/client'
import { FloodProvider } from './components/FloodProvider'
import { ToastProvider } from './components/ToastProvider'
import { pollWhileStreamDown } from './hooks/useLiveEvents'
import { router } from './router'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5_000,
      refetchInterval: pollWhileStreamDown,
      // A 4xx won't fix itself on retry, for example an endpoint the API doesn't have yet.
      retry: (failures, error) => !(error instanceof ApiError && error.status < 500) && failures < 2,
    },
  },
})

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <FloodProvider>
          <RouterProvider router={router} />
        </FloodProvider>
      </ToastProvider>
    </QueryClientProvider>
  )
}

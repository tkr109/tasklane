import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import './styles/index.css'

async function startMocksIfAsked() {
  if (import.meta.env.VITE_MOCK !== '1') return
  const { startMockApi } = await import('./mocks/browser')
  await startMockApi()
}

startMocksIfAsked().then(() => {
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  )
})

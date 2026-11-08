import { network } from 'virtual:msw'
import { handlers } from './handlers'

export async function startMockApi() {
  // The simulation polls and streams constantly; logging each request would bury real errors.
  network.configure({ handlers, context: { quiet: true } })
  await network.enable()
}

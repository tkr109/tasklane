import { network } from 'virtual:msw'
import { handlers } from './handlers'

export async function startMockApi() {
  network.configure({ handlers })
  await network.enable()
}

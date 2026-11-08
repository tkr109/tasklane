import type { JobSpec } from '../api/types'

export const FLOOD_SIZE = 10_000

// The API takes at most 500 jobs per batch.
export const FLOOD_BATCH = 500

export const FLOOD_JOB: JobSpec = {
  type: 'image.resize',
  priority: 3,
  payload: { source: 's3://loadtest/uploads/flood.jpg', sizes: [320, 640, 1280], format: 'webp' },
}

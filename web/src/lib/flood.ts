import type { JobSpec } from '../api/types'

export const FLOOD_SIZE = 10_000

// The API takes at most 500 jobs per batch.
export const FLOOD_BATCH = 500

// A built-in sample image, so the flood needs no network. The handler writes jpeg
// or png only and never upscales past the source.
export const FLOOD_JOB: JobSpec = {
  type: 'image.resize',
  priority: 3,
  payload: { source: 'sample:gradient', sizes: [320, 640], format: 'jpeg' },
}

-- +goose Up
-- The overview counts today's successes once a second; this keeps that to a range
-- scan of one index however many jobs have finished over time.
create index jobs_succeeded_idx on jobs (finished_at) where status = 'succeeded';

-- +goose Down
drop index jobs_succeeded_idx;

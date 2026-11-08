-- +goose Up
-- When a job's current wait began. Aging counts from here. It moves with run_at,
-- except when a rate limit pushes the job back, so the job keeps the priority it
-- has built up by waiting.
alter table jobs add column ready_at timestamptz;
update jobs set ready_at = run_at;
alter table jobs alter column ready_at set not null, alter column ready_at set default now();

-- Grabs take the job that has waited longest at a level, so order by ready_at.
-- Rows still in a hold have ready_at < run_at, and they are the only ones a scan
-- skips before finding a runnable job.
drop index jobs_team_runnable_idx;
create index jobs_team_runnable_idx on jobs (team_id, priority, ready_at, id)
    where status in ('queued', 'failed');

-- +goose Down
drop index jobs_team_runnable_idx;
create index jobs_team_runnable_idx on jobs (team_id, priority, run_at, id)
    where status in ('queued', 'failed');
alter table jobs drop column ready_at;

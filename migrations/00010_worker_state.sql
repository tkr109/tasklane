-- +goose Up
-- A worker drains when it is told to stop: it finishes what it has and takes
-- nothing new. jobs_done is how many jobs it has finished, sent with each heartbeat.
alter table workers
    add column state text not null default 'running' check (state in ('running', 'draining')),
    add column jobs_done bigint not null default 0;

-- +goose Down
alter table workers drop column state, drop column jobs_done;

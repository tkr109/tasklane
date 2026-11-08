-- +goose Up
-- A schedule queues a job from its template every time its cron expression fires.
create table schedules (
    id           bigint generated always as identity primary key,
    team_id      bigint not null references teams (id) on delete cascade,
    cron         text not null,
    timezone     text not null default 'UTC',
    type         text not null,
    payload      jsonb not null default '{}',
    priority     smallint not null default 5 check (priority between 0 and 9),
    max_attempts int not null default 3 check (max_attempts > 0),
    enabled      boolean not null default true,
    next_run_at  timestamptz not null,
    last_run_at  timestamptz,
    created_at   timestamptz not null default now(),
    updated_at   timestamptz not null default now()
);

create index schedules_due_idx on schedules (next_run_at) where enabled;
create index schedules_team_idx on schedules (team_id, id);

-- +goose Down
drop table schedules;

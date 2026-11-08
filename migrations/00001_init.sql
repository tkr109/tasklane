-- +goose Up
create table plans (
    id              bigint generated always as identity primary key,
    name            text not null unique,
    share_weight    int not null,
    max_workers     int not null,
    jobs_per_minute int not null,
    burst           int not null
);

create table teams (
    id         bigint generated always as identity primary key,
    name       text not null unique,
    plan_id    bigint not null references plans (id),
    created_at timestamptz not null default now()
);

create table api_keys (
    id         bigint generated always as identity primary key,
    team_id    bigint not null references teams (id),
    prefix     text not null unique,
    hash       bytea not null,
    created_at timestamptz not null default now(),
    revoked_at timestamptz
);

create table jobs (
    id              bigint generated always as identity primary key,
    team_id         bigint not null references teams (id),
    type            text not null,
    payload         jsonb not null default '{}',
    priority        smallint not null default 5 check (priority between 0 and 9),
    status          text not null default 'queued'
        check (status in ('queued', 'running', 'succeeded', 'failed', 'dead', 'cancelled')),
    attempts        int not null default 0,
    max_attempts    int not null default 3 check (max_attempts > 0),
    run_at          timestamptz not null default now(),
    lease_until     timestamptz,
    locked_by       text,
    idempotency_key text,
    progress        smallint not null default 0 check (progress between 0 and 100),
    last_error      text,
    created_at      timestamptz not null default now(),
    updated_at      timestamptz not null default now(),
    started_at      timestamptz,
    finished_at     timestamptz,
    unique (team_id, idempotency_key)
);

-- 'failed' jobs are waiting out their backoff, so they are runnable once run_at passes.
create index jobs_runnable_idx on jobs (priority desc, run_at, id)
    where status in ('queued', 'failed');
create index jobs_lease_idx on jobs (lease_until) where status = 'running';
create index jobs_team_idx on jobs (team_id, id desc);

create table job_events (
    id      bigint generated always as identity primary key,
    job_id  bigint not null references jobs (id) on delete cascade,
    kind    text not null,
    message text not null default '',
    at      timestamptz not null default now()
);

create index job_events_job_idx on job_events (job_id, id);

create table workers (
    id           text primary key,
    host         text not null,
    started_at   timestamptz not null default now(),
    last_seen_at timestamptz not null default now()
);

-- +goose Down
drop table workers;
drop table job_events;
drop table jobs;
drop table api_keys;
drop table teams;
drop table plans;

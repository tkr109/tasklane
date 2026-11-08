-- +goose Up
-- Token buckets: one per team for submits ('team:<id>') and one per job type with
-- an outgoing limit ('type:<name>'). Tokens are refilled from updated_at on use.
create table rate_buckets (
    key        text primary key,
    tokens     double precision not null,
    updated_at timestamptz not null default now()
);

-- When a worker last pushed the job back because its type was out of tokens.
alter table jobs add column throttled_at timestamptz;
create index jobs_throttled_idx on jobs (throttled_at) where throttled_at is not null;

-- The teams page shows how many jobs each team sent in the last minute.
create index jobs_team_created_idx on jobs (team_id, created_at);

-- +goose Down
drop index jobs_team_created_idx;
drop index jobs_throttled_idx;
alter table jobs drop column throttled_at;
drop table rate_buckets;

-- +goose Up
create table team_usage (
    team_id   bigint primary key references teams (id) on delete cascade,
    running   int not null default 0,
    -- Jobs started recently. It shrinks by a factor of e every fair-share window,
    -- and the grab that bumps it applies the decay since served_at first.
    served    double precision not null default 0,
    served_at timestamptz not null default now()
);

insert into team_usage (team_id, running)
select t.id, count(j.id)
from teams t
left join jobs j on j.team_id = t.id and j.status = 'running'
group by t.id;

-- +goose StatementBegin
create function track_running() returns trigger language plpgsql as $$
begin
    if new.status = 'running' then
        update team_usage set running = running + 1 where team_id = new.team_id;
    else
        update team_usage set running = running - 1 where team_id = old.team_id;
    end if;
    return null;
end
$$;
-- +goose StatementEnd

-- Whatever moves a job in or out of running, the team's count follows.
create trigger jobs_track_running
    after update of status on jobs
    for each row
    when ((old.status = 'running') <> (new.status = 'running'))
    execute function track_running();

-- Grabs look up the oldest runnable job per team and priority.
drop index jobs_runnable_idx;
create index jobs_team_runnable_idx on jobs (team_id, priority, run_at, id)
    where status in ('queued', 'failed');

-- +goose Down
drop index jobs_team_runnable_idx;
create index jobs_runnable_idx on jobs (priority desc, run_at, id)
    where status in ('queued', 'failed');
drop trigger jobs_track_running on jobs;
drop function track_running();
drop table team_usage;

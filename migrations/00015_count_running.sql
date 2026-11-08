-- +goose Up
-- A team's running jobs are counted rather than kept in team_usage. Keeping the
-- counter made every finish wait for its team's row behind the team's other grabs
-- and finishes. Now finishes leave the row alone, and grabs count with this index.
create index jobs_running_idx on jobs (team_id) where status = 'running';
drop trigger jobs_track_running on jobs;
drop function track_running();
alter table team_usage drop column running;

-- +goose Down
alter table team_usage add column running int not null default 0;
update team_usage u
set running = (select count(*) from jobs j where j.team_id = u.team_id and j.status = 'running');

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

create trigger jobs_track_running
    after update of status on jobs
    for each row
    when ((old.status = 'running') <> (new.status = 'running'))
    execute function track_running();
drop index jobs_running_idx;

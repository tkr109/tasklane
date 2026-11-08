-- +goose Up
-- Every change to a job's status or progress is announced on tasklane_events, the
-- channel the api relays to dashboards. A trigger catches every path at once:
-- submits, grabs, finishes, holds, the reaper and the scheduler. The notification
-- goes out when the transaction commits, and not at all if it rolls back.
-- +goose StatementBegin
create function notify_job() returns trigger language plpgsql as $$
begin
    -- Text fields are cut short: a payload over 8000 bytes would fail the write.
    perform pg_notify('tasklane_events', jsonb_build_object(
        'event', 'job',
        'data', jsonb_build_object(
            'id', new.id,
            'team_id', new.team_id,
            'type', left(new.type, 100),
            'status', new.status,
            'progress', new.progress,
            'attempts', new.attempts,
            'run_at', new.run_at,
            'locked_by', left(new.locked_by, 100),
            'last_error', left(new.last_error, 200),
            'updated_at', new.updated_at
        )
    )::text);
    return null;
end
$$;
-- +goose StatementEnd

create trigger jobs_notify_insert
    after insert on jobs
    for each row
    execute function notify_job();

create trigger jobs_notify_update
    after update of status, progress on jobs
    for each row
    when (old.status is distinct from new.status or old.progress is distinct from new.progress)
    execute function notify_job();

-- +goose Down
drop trigger jobs_notify_update on jobs;
drop trigger jobs_notify_insert on jobs;
drop function notify_job();

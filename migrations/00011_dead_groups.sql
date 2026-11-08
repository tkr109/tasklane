-- +goose Up
-- normalize_error reduces an error message to its shape, so the same failure with
-- a different id, address, number or quoted value lands in one dead job group.
-- +goose StatementBegin
create function normalize_error(msg text) returns text language sql immutable as $$
    select left(
        regexp_replace(
            regexp_replace(
                regexp_replace(
                    regexp_replace(coalesce(msg, ''),
                        '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}', '<id>', 'gi'),
                    '"[^"]*"|''[^'']*''', '<str>', 'g'),
                '[^\s@]+@[^\s@]+', '<email>', 'g'),
            '\m\w*[0-9]\w*\M', '<n>', 'g'),
        200)
$$;
-- +goose StatementEnd

-- Dead jobs are grouped by type and error_key. A trigger fills it in, so every
-- way a job can die (a last failed attempt, the reaper) is covered.
alter table jobs add column error_key text;
update jobs set error_key = normalize_error(last_error) where status = 'dead';

-- +goose StatementBegin
create function set_error_key() returns trigger language plpgsql as $$
begin
    new.error_key := normalize_error(new.last_error);
    return new;
end
$$;
-- +goose StatementEnd

create trigger jobs_error_key
    before insert or update of status, last_error on jobs
    for each row
    when (new.status = 'dead')
    execute function set_error_key();

-- The groups page, the counts and the retry of a group all read dead jobs by
-- these columns, so they can be answered from the index alone.
create index jobs_dead_idx on jobs (type, error_key, finished_at, team_id) where status = 'dead';

-- +goose Down
drop index jobs_dead_idx;
drop trigger jobs_error_key on jobs;
drop function set_error_key();
alter table jobs drop column error_key;
drop function normalize_error(text);

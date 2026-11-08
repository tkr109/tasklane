-- +goose Up
-- The scaler adds a worker's row as starting when it starts its container, and
-- the worker's first heartbeat makes it running.
alter table workers drop constraint workers_state_check,
    add constraint workers_state_check check (state in ('starting', 'running', 'draining'));

-- +goose Down
delete from workers where state = 'starting';
alter table workers drop constraint workers_state_check,
    add constraint workers_state_check check (state in ('running', 'draining'));

-- +goose Up
insert into plans (name, share_weight, max_workers, jobs_per_minute, burst) values
    ('free', 1, 2, 100, 200),
    ('pro', 3, 6, 1000, 2000),
    ('enterprise', 10, 10, 10000, 20000);

-- Local runs and the dev API key use this team.
insert into teams (name, plan_id)
select 'default', id from plans where name = 'pro';

-- +goose Down
delete from teams where name = 'default';
delete from plans where name in ('free', 'pro', 'enterprise');

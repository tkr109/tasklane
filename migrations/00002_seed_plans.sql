-- +goose Up
insert into plans (name, share_weight, max_workers, jobs_per_minute, burst) values
    ('free', 1, 2, 60, 20),
    ('pro', 3, 10, 600, 200),
    ('enterprise', 10, 50, 6000, 2000);

-- Until API keys land, every request runs as this team.
insert into teams (name, plan_id)
select 'default', id from plans where name = 'pro';

-- +goose Down
delete from teams where name = 'default';
delete from plans where name in ('free', 'pro', 'enterprise');

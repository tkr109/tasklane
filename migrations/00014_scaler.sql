-- +goose Up
-- The scaler's decisions for the dashboard's log. It keeps the latest 200.
create table scaler_decisions (
    id           bigserial primary key,
    at           timestamptz not null default now(),
    action       text not null check (action in ('up', 'down', 'hold')),
    from_workers int not null,
    to_workers   int not null,
    reason       text not null
);

-- What the leading scaler reported last, read by the overview. One row at most.
create table scaler_status (
    id         boolean primary key default true check (id),
    status     jsonb not null,
    updated_at timestamptz not null default now()
);

-- +goose Down
drop table scaler_status;
drop table scaler_decisions;
